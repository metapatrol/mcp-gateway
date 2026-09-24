package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
)

// TestInspectTokenReview checks the inspect API's authentication against a
// real API server: tokens the kubelet would project, reviewed as the gateway
// reviews them. The unit tests fake the answers; this is where the shape of a
// real one — username, audiences — is confirmed.
func TestInspectTokenReview(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"mcp-gateway-ui", "someone-else"} {
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		require.NoError(t, k8sClient.Create(ctx, sa))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, sa) })
	}
	token := func(account, audience string) string {
		t.Helper()
		tr := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{Audiences: []string{audience}}}
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: account, Namespace: "default"}}
		require.NoError(t, k8sClient.SubResource("token").Create(ctx, sa, tr))
		require.NotEmpty(t, tr.Status.Token)
		return tr.Status.Token
	}

	reviewer := &inspect.TokenReviewer{
		Client:          k8sClient,
		Audience:        "mcp-gateway-inspect",
		ServiceAccounts: []string{"default/mcp-gateway-ui"},
	}
	h := reviewer.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1alpha1/servers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusNoContent, call(token("mcp-gateway-ui", "mcp-gateway-inspect")))
	assert.Equal(t, http.StatusUnauthorized, call(token("mcp-gateway-ui", "some-other-api")), "another audience")
	assert.Equal(t, http.StatusUnauthorized, call(token("someone-else", "mcp-gateway-inspect")), "another ServiceAccount")
	assert.Equal(t, http.StatusUnauthorized, call("not-a-token"))
}

// TestInspectEvents checks that the event stream hears what the console needs
// to hear from a real API server: resources created, status written, and a
// catalog this replica discovers. No controller runs, so every status write is
// the test's own and every event can be told apart.
func TestInspectEvents(t *testing.T) {
	ctx := context.Background()
	_, _, events := startGatewayWithInspect(t)
	stream, cancel, err := events.Subscribe()
	require.NoError(t, err)
	defer cancel()

	seen := map[api.Change]bool{}
	waitFor := func(want api.Change) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !seen[want] {
			select {
			case batch := <-stream:
				for _, c := range batch.Changes {
					seen[c] = true
				}
			case <-deadline:
				t.Fatalf("no event for %v; saw %v", want, seen)
			}
		}
	}

	grant := &mcpv1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "events-grant"},
		Spec:       mcpv1alpha1.MCPGrantSpec{Subject: "someone", Capabilities: []string{"nothing"}},
	}
	require.NoError(t, k8sClient.Create(ctx, grant))
	t.Cleanup(func() { _ = k8sClient.Delete(ctx, grant) })
	waitFor(api.Change{Kind: api.ChangeKindGrant, Name: "events-grant"})

	// A status write — which the gateway's own reconcilers do not watch for —
	// reaches the stream.
	delete(seen, api.Change{Kind: api.ChangeKindGrant, Name: "events-grant"})
	grant.Status.Conditions = []metav1.Condition{{
		Type: "Valid", Status: metav1.ConditionFalse, Reason: "CapabilityNotFound", Message: "nothing", LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, k8sClient.Status().Update(ctx, grant))
	waitFor(api.Change{Kind: api.ChangeKindGrant, Name: "events-grant"})

	backend := mcptest.NewBackend(t, "events-backend", "one")
	server := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "events-server"},
		Spec:       mcpv1alpha1.MCPServerSpec{Endpoint: backend.URL},
	}
	require.NoError(t, k8sClient.Create(ctx, server))
	t.Cleanup(func() { _ = k8sClient.Delete(ctx, server) })
	waitFor(api.Change{Kind: api.ChangeKindServer, Name: "events-server"})

	// A tool added on the backend is a new catalog on this replica, which
	// no write to the API announces.
	time.Sleep(time.Second) // let the first discovery's event pass
	delete(seen, api.Change{Kind: api.ChangeKindServer, Name: "events-server"})
	backend.AddTool("two")
	waitFor(api.Change{Kind: api.ChangeKindServer, Name: "events-server"})
}

// TestInspectToolsListIsWhatAgentsGet compares a grant's tools/list in the
// inspect API with the tools/list an agent holding only that grant receives
// from the gateway: the console's JSON view has to be the real thing, prefix,
// aliases, descriptions, schemas and hints included.
func TestInspectToolsListIsWhatAgentsGet(t *testing.T) {
	ctx := context.Background()
	mcpURL, inspectURL, _ := startGatewayWithInspect(t)

	backend := mcptest.NewBackend(t, "fidelity", "plain")
	backend.AddDefinedTool(&mcp.Tool{
		Name:        "search",
		Title:       "Search",
		Description: "Search the things.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"what"}},"required":["q"]}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: ptrTo(false)},
	})
	for _, obj := range []client.Object{
		&mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: "fidelity"}, Spec: mcpv1alpha1.MCPServerSpec{Endpoint: backend.URL}},
		&mcpv1alpha1.MCPToolSet{ObjectMeta: metav1.ObjectMeta{Name: "fidelity"}, Spec: mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "fidelity", Tools: []string{"*"}, Prefix: "f_", Aliases: map[string]mcpv1alpha1.ToolName{"search": "find"},
		}},
		&mcpv1alpha1.MCPCapability{ObjectMeta: metav1.ObjectMeta{Name: "fidelity"}, Spec: mcpv1alpha1.MCPCapabilitySpec{ToolSets: []string{"fidelity"}}},
		&mcpv1alpha1.MCPGrant{ObjectMeta: metav1.ObjectMeta{Name: "fidelity"}, Spec: mcpv1alpha1.MCPGrantSpec{Subject: "fidelity-agent", Capabilities: []string{"fidelity"}}},
	} {
		require.NoError(t, k8sClient.Create(ctx, obj))
		t.Cleanup(func() { _ = k8sClient.Delete(ctx, obj) })
	}

	session := connect(t, mcpURL, "fidelity-agent", nil)
	var agent []byte
	require.Eventually(t, func() bool {
		res, err := session.ListTools(ctx, nil)
		if err != nil || len(res.Tools) != 2 {
			return false
		}
		agent, err = json.Marshal(res.Tools)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond)

	resp, err := http.Get(inspectURL + "/api/v1alpha1/grants/fidelity/tools")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var doc struct {
		Tools json.RawMessage `json:"tools"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))

	assert.JSONEq(t, string(agent), string(doc.Tools))
	assert.Contains(t, string(doc.Tools), `"name":"find"`)
	assert.Contains(t, string(doc.Tools), `"name":"f_plain"`)
}
