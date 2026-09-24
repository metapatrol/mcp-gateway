package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestInspectMCPIsWhatAgentsGet compares a grant's MCP view in the inspect API
// with what the gateway answers an agent holding only that grant: the same
// JSON-RPC responses to server/discover and tools/list, key for key — prefix,
// aliases, descriptions, schemas, hints, cache scope and instructions
// included.
func TestInspectMCPIsWhatAgentsGet(t *testing.T) {
	ctx := context.Background()
	mcpURL, inspectURL, _ := startGatewayWithInspect(t)

	backend := mcptest.NewBackendWithInstructions(t, "fidelity", "Search first.", "plain")
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

	// What the agent receives, as raw as it arrives.
	call := func(method string) string {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
			`"io.modelcontextprotocol/clientInfo":{"name":"mcp-gateway-inspect","version":"preview"},` +
			`"io.modelcontextprotocol/clientCapabilities":{}}}}`
		req, _ := http.NewRequest(http.MethodPost, mcpURL, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer fidelity-agent")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2026-07-28")
		req.Header.Set("Mcp-Method", method)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		out := string(raw)
		for line := range strings.SplitSeq(out, "\n") {
			if data, ok := strings.CutPrefix(line, "data:"); ok {
				out = strings.TrimSpace(data)
			}
		}
		return out
	}
	var agentList string
	require.Eventually(t, func() bool {
		agentList = call("tools/list")
		return strings.Contains(agentList, `"name":"find"`)
	}, 10*time.Second, 100*time.Millisecond)

	resp, err := http.Get(inspectURL + "/api/v1alpha1/grants/fidelity/mcp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var ex struct {
		Discover  json.RawMessage   `json:"discover"`
		ToolsList []json.RawMessage `json:"toolsList"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ex))
	require.Len(t, ex.ToolsList, 1)

	assert.Equal(t, call("server/discover"), string(ex.Discover), "byte for byte")
	assert.Equal(t, agentList, string(ex.ToolsList[0]), "byte for byte")
	assert.Contains(t, string(ex.Discover), "Search first.")
	assert.Contains(t, agentList, `"name":"f_plain"`)

	server, err := http.Get(inspectURL + "/api/v1alpha1/servers/fidelity/mcp")
	require.NoError(t, err)
	defer func() { _ = server.Body.Close() }()
	var backendView struct {
		Handshake json.RawMessage   `json:"handshake"`
		ToolsList []json.RawMessage `json:"toolsList"`
	}
	require.NoError(t, json.NewDecoder(server.Body).Decode(&backendView))
	assert.Contains(t, string(backendView.Handshake), `"name":"fidelity"`, "the backend's own server info")
	require.NotEmpty(t, backendView.ToolsList)
	assert.Contains(t, string(backendView.ToolsList[0]), `"name":"search"`, "the backend's names")
}
