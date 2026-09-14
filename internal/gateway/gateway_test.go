package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/metrics"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/policy"
)

// tokens maps bearer tokens to subjects. Token verification has its own tests;
// here a token is simply a name for a subject.
var tokens = map[string]string{
	"token-coding-agent": "coding-agent",
	"token-reviewer":     "reviewer",
	"token-nobody":       "nobody",
}

func fakeVerifier(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	subject, ok := tokens[token]
	if !ok {
		return nil, sdkauth.ErrInvalidToken
	}
	return &sdkauth.TokenInfo{UserID: subject, Expiration: time.Now().Add(time.Hour)}, nil
}

// env is a gateway in front of two backends, as CONCEPT.md draws it.
type env struct {
	t        *testing.T
	registry *discovery.Registry
	gateway  *Gateway
	url      string

	sourceIntelligence *mcptest.Backend
	artifact           *mcptest.Backend

	servers      []gatev1alpha1.MCPServer
	toolSets     []gatev1alpha1.MCPToolSet
	capabilities []gatev1alpha1.MCPCapability
	grants       []gatev1alpha1.MCPGrant
}

func newEnv(t *testing.T, opts Options) *env {
	t.Helper()
	e := &env{t: t}
	e.sourceIntelligence = mcptest.NewBackend(t, "source-intelligence", "source_search", "source_get", "symbol_search", "repository_search")
	e.artifact = mcptest.NewBackend(t, "artifact", "artifact_search", "artifact_get", "artifact_publish")

	e.registry = discovery.NewRegistry(discovery.Options{ConnectTimeout: 2 * time.Second})
	t.Cleanup(e.registry.Close)
	opts.Registry = e.registry
	opts.Logger = logr.Discard()
	e.gateway = New(opts)

	handler, err := e.gateway.Handler(HandlerOptions{
		Verifier:             fakeVerifier,
		ResourceURL:          "https://mcp-gate.example.com/mcp",
		AuthorizationServers: []string{"https://oauth.example.com/"},
	})
	require.NoError(t, err)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	e.url = srv.URL + "/mcp"

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = e.gateway.Start(ctx) }()

	e.register("source-intelligence", e.sourceIntelligence.URL)
	e.register("artifact", e.artifact.URL)
	e.toolSets = []gatev1alpha1.MCPToolSet{
		toolSet("source-intelligence-analysis", "source-intelligence", "source_search", "source_get", "symbol_search"),
		toolSet("artifact-read", "artifact", "artifact_search", "artifact_get"),
		toolSet("artifact-readwrite", "artifact", "artifact_search", "artifact_get", "artifact_publish"),
	}
	e.capabilities = []gatev1alpha1.MCPCapability{
		capability("code-research", "source-intelligence-analysis", "artifact-read"),
		capability("artifact-authoring", "artifact-readwrite"),
	}
	e.grants = []gatev1alpha1.MCPGrant{
		grant("coding-agent", "coding-agent", "code-research"),
		grant("reviewer", "reviewer", "artifact-authoring"),
	}
	e.apply()
	return e
}

func (e *env) register(name, url string) {
	e.t.Helper()
	e.servers = append(e.servers, gatev1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: name}})
	e.registry.Ensure(name, url)
	_, _, err := e.registry.Discover(context.Background(), name)
	require.NoError(e.t, err)
}

// apply hands the current manifests to the gateway, the way the policy
// reconciler does.
func (e *env) apply() {
	e.gateway.SetPolicy(policy.NewSnapshot(e.servers, e.toolSets, e.capabilities, e.grants))
}

func (e *env) connect(token string) *mcp.ClientSession {
	e.t.Helper()
	session, err := e.connectWith(token, nil)
	require.NoError(e.t, err)
	return session
}

func (e *env) connectWith(token string, opts *mcp.ClientOptions) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   e.url,
		HTTPClient: &http.Client{Transport: bearer(token)},
	}, nil)
	if err == nil {
		e.t.Cleanup(func() { _ = session.Close() })
	}
	return session, err
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func call(session *mcp.ClientSession, name string) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"q": "x"}})
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func toolSet(name, server string, tools ...string) gatev1alpha1.MCPToolSet {
	return gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: server, Tools: tools},
	}
}

func capability(name string, toolSets ...string) gatev1alpha1.MCPCapability {
	return gatev1alpha1.MCPCapability{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: toolSets},
	}
}

func grant(name, subject string, capabilities ...string) gatev1alpha1.MCPGrant {
	return gatev1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPGrantSpec{Subject: subject, Capabilities: capabilities},
	}
}

func TestToolsList_IsTheAggregatedGrantedSubset(t *testing.T) {
	e := newEnv(t, Options{})

	assert.Equal(t,
		[]string{"artifact_get", "artifact_search", "source_get", "source_search", "symbol_search"},
		toolNames(t, e.connect("token-coding-agent")))
	assert.Equal(t,
		[]string{"artifact_get", "artifact_publish", "artifact_search"},
		toolNames(t, e.connect("token-reviewer")))
	assert.Empty(t, toolNames(t, e.connect("token-nobody")), "a subject without a grant sees nothing")
}

// The gateway passes the backend's definition on: agents pick tools by their
// descriptions and fill them in by their schemas.
func TestToolsList_CarriesTheBackendDefinition(t *testing.T) {
	e := newEnv(t, Options{})
	res, err := e.connect("token-coding-agent").ListTools(context.Background(), nil)
	require.NoError(t, err)

	i := slices.IndexFunc(res.Tools, func(tool *mcp.Tool) bool { return tool.Name == "artifact_get" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "artifact_get from artifact", res.Tools[i].Description)
	schema, err := json.Marshal(res.Tools[i].InputSchema)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"object"}`, string(schema))
}

func TestToolsCall_IsRoutedToTheOwningBackend(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-coding-agent")

	res, err := call(session, "source_search")
	require.NoError(t, err)
	assert.Equal(t, `source-intelligence/source_search {"q":"x"}`, text(res))

	res, err = call(session, "artifact_get")
	require.NoError(t, err)
	assert.Equal(t, `artifact/artifact_get {"q":"x"}`, text(res))
}

// A tool the backend has and the grant does not is refused — in exactly the
// words used for a tool that does not exist anywhere.
func TestToolsCall_OutsideTheGrantIsIndistinguishableFromNonexistent(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-coding-agent")
	denied := testutil.ToFloat64(metrics.DeniedToolCalls.WithLabelValues("coding-agent"))

	_, errForbidden := call(session, "artifact_publish")
	_, errNonexistent := call(session, "no_such_tool")
	require.Error(t, errForbidden)
	require.Error(t, errNonexistent)
	assert.Equal(t,
		strings.ReplaceAll(errNonexistent.Error(), "no_such_tool", "X"),
		strings.ReplaceAll(errForbidden.Error(), "artifact_publish", "X"))
	assert.Zero(t, e.artifact.Calls.Load(), "the refused call must not reach the backend")
	assert.Equal(t, denied+2, testutil.ToFloat64(metrics.DeniedToolCalls.WithLabelValues("coding-agent")),
		"refusals are counted even when the SDK turns the call away before the gateway's handler runs")
}

// Authorisation is decided per call. A grant revoked while a session is open
// takes effect on that session's next call, and the session is told its tool
// list changed.
func TestToolsCall_RevocationTakesEffectOnAnOpenSession(t *testing.T) {
	e := newEnv(t, Options{})
	changed := make(chan struct{}, 8)
	session, err := e.connectWith("token-coding-agent", &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} },
	})
	require.NoError(t, err)
	_, err = call(session, "artifact_get")
	require.NoError(t, err)

	e.grants = []gatev1alpha1.MCPGrant{grant("reviewer", "reviewer", "artifact-authoring")}
	e.apply()

	require.Eventually(t, func() bool {
		_, err := call(session, "artifact_get")
		return err != nil
	}, 5*time.Second, 20*time.Millisecond)
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the session was not told that its tools changed")
	}
	assert.Empty(t, toolNames(t, session))
}

// The per-call check does not trust the tool list. Here the list is stale on
// purpose — the sync that would update it never runs — and the call is still
// refused.
func TestToolsCall_IsAuthorisedAgainstThePolicyNotTheList(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-coding-agent")
	require.Contains(t, toolNames(t, session), "artifact_get")

	e.gateway.policy.Store(policy.Empty()) // no Invalidate: the list stays as it was
	assert.Contains(t, toolNames(t, session), "artifact_get")
	_, err := call(session, "artifact_get")
	assert.Error(t, err)
}

func TestNewGrantAppearsOnAnOpenSession(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-nobody")
	require.Empty(t, toolNames(t, session))

	e.grants = append(e.grants, grant("nobody", "nobody", "artifact-authoring"))
	e.apply()

	require.Eventually(t, func() bool { return len(toolNames(t, session)) == 3 }, 5*time.Second, 20*time.Millisecond)
	res, err := call(session, "artifact_publish")
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

// One broken backend is not a global outage.
func TestBackendDown_OtherBackendsKeepWorking(t *testing.T) {
	e := newEnv(t, Options{CatalogMaxAge: time.Hour})
	session := e.connect("token-coding-agent")

	e.sourceIntelligence.Close()

	res, err := call(session, "artifact_get")
	require.NoError(t, err)
	assert.False(t, res.IsError)

	res, err = call(session, "source_search")
	require.NoError(t, err, "a backend failure is a tool error, which the model can see and act on")
	assert.True(t, res.IsError)
	assert.NotContains(t, text(res), e.sourceIntelligence.URL, "backend addresses must not leak to callers")
}

// A backend that stays down long enough drops out of the tool lists, and comes
// back when it recovers.
func TestBackendDown_ToolsDisappearAfterMaxAgeAndReturn(t *testing.T) {
	e := newEnv(t, Options{CatalogMaxAge: time.Nanosecond})
	session := e.connect("token-coding-agent")
	require.Len(t, toolNames(t, session), 5)

	e.artifact.Close()
	_, _, err := e.registry.Discover(context.Background(), "artifact")
	require.Error(t, err)
	e.gateway.Invalidate()

	require.Eventually(t, func() bool {
		return slices.Equal(toolNames(t, session), []string{"source_get", "source_search", "symbol_search"})
	}, 5*time.Second, 20*time.Millisecond)

	revived := mcptest.NewBackend(t, "artifact", "artifact_search", "artifact_get", "artifact_publish")
	e.registry.Ensure("artifact", revived.URL)
	_, _, err = e.registry.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	e.gateway.Invalidate()

	require.Eventually(t, func() bool { return len(toolNames(t, session)) == 5 }, 5*time.Second, 20*time.Millisecond)
}

// A tool that disappears from the backend is no longer exposed, and the rest of
// its tool set is.
func TestMissingToolIsNotExposed(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-coding-agent")

	e.sourceIntelligence.RemoveTool("symbol_search")
	_, changed, err := e.registry.Discover(context.Background(), "source-intelligence")
	require.NoError(t, err)
	require.True(t, changed)
	e.gateway.Invalidate()

	require.Eventually(t, func() bool {
		return !slices.Contains(toolNames(t, session), "symbol_search")
	}, 5*time.Second, 20*time.Millisecond)
	assert.Contains(t, toolNames(t, session), "source_search")
	_, err = call(session, "symbol_search")
	assert.Error(t, err)
}

func TestNameConflictExposesNeither(t *testing.T) {
	e := newEnv(t, Options{})
	engram := mcptest.NewBackend(t, "engram", "artifact_get", "remember")
	e.register("engram", engram.URL)
	e.toolSets = append(e.toolSets, toolSet("memory", "engram", "artifact_get", "remember"))
	e.capabilities = append(e.capabilities, capability("memory", "memory"))
	e.grants[0].Spec.Capabilities = append(e.grants[0].Spec.Capabilities, "memory")
	e.apply()

	session := e.connect("token-coding-agent")
	names := toolNames(t, session)
	assert.NotContains(t, names, "artifact_get")
	assert.Contains(t, names, "remember")
	_, err := call(session, "artifact_get")
	assert.Error(t, err)
}

func TestFailingToolIsPassedOn(t *testing.T) {
	e := newEnv(t, Options{})
	e.artifact.AddFailingTool("artifact_get")
	_, _, err := e.registry.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	e.gateway.Invalidate()

	session := e.connect("token-coding-agent")
	require.Eventually(t, func() bool {
		res, err := call(session, "artifact_get")
		return err == nil && res.IsError && text(res) == "it broke"
	}, 5*time.Second, 20*time.Millisecond)
}

func TestUnauthenticatedRequestsGetAChallenge(t *testing.T) {
	e := newEnv(t, Options{})

	for _, token := range []string{"", "not-a-known-token"} {
		req, err := http.NewRequest(http.MethodPost, e.url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("WWW-Authenticate"),
			`resource_metadata="https://mcp-gate.example.com/.well-known/oauth-protected-resource/mcp"`)
	}

	_, err := e.connectWith("not-a-known-token", nil)
	assert.Error(t, err)
}

// A session belongs to the subject that opened it. Continuing it with another
// subject's token is refused, so a leaked session ID is not a way into someone
// else's grants.
func TestSessionCannotBeContinuedByAnotherSubject(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-reviewer")

	req, err := http.NewRequest(http.MethodPost, e.url, strings.NewReader(
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"artifact_publish","arguments":{}}}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", session.ID())
	req.Header.Set("Mcp-Protocol-Version", session.InitializeResult().ProtocolVersion)
	req.Header.Set("Authorization", "Bearer token-coding-agent")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if session.ID() != "" {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	}
	assert.Zero(t, e.artifact.Calls.Load())
}

func TestProtectedResourceMetadata(t *testing.T) {
	e := newEnv(t, Options{})
	base := strings.TrimSuffix(e.url, "/mcp")

	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		resp, err := http.Get(base + path)
		require.NoError(t, err)
		var metadata map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&metadata))
		_ = resp.Body.Close()
		assert.Equal(t, "https://mcp-gate.example.com/mcp", metadata["resource"])
		assert.Equal(t, []any{"https://oauth.example.com/"}, metadata["authorization_servers"])
	}
}

func TestStatelessMode(t *testing.T) {
	e := newEnv(t, Options{})
	handler, err := e.gateway.Handler(HandlerOptions{Verifier: fakeVerifier, Stateless: true})
	require.NoError(t, err)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	e.url = srv.URL + "/mcp"

	session := e.connect("token-reviewer")
	assert.Equal(t, []string{"artifact_get", "artifact_publish", "artifact_search"}, toolNames(t, session))
	res, err := call(session, "artifact_publish")
	require.NoError(t, err)
	assert.Equal(t, `artifact/artifact_publish {"q":"x"}`, text(res))
	_, err = call(session, "source_search")
	assert.Error(t, err)
}

// A tool list is one subject's. Marked public, an intermediary could serve it to
// the next caller.
func TestToolsList_IsMarkedPrivateForCaching(t *testing.T) {
	e := newEnv(t, Options{})
	res, err := e.connect("token-coding-agent").ListTools(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "private", res.CacheScope)
}

func TestReadyOnlyOncePolicyIsLoaded(t *testing.T) {
	g := New(Options{Registry: discovery.NewRegistry(discovery.Options{}), Logger: logr.Discard()})
	assert.Error(t, g.Ready(nil))
	g.SetPolicy(policy.Empty())
	assert.NoError(t, g.Ready(nil))
}

func TestProtectedResourceMetadataURL(t *testing.T) {
	for resource, want := range map[string]string{
		"https://gw.example.com/mcp":  "https://gw.example.com/.well-known/oauth-protected-resource/mcp",
		"https://gw.example.com/mcp/": "https://gw.example.com/.well-known/oauth-protected-resource/mcp",
		"https://gw.example.com":      "https://gw.example.com/.well-known/oauth-protected-resource",
	} {
		got, err := protectedResourceMetadataURL(resource)
		require.NoError(t, err)
		assert.Equal(t, want, got, resource)
	}
	_, err := protectedResourceMetadataURL("/relative")
	assert.Error(t, err)
}
