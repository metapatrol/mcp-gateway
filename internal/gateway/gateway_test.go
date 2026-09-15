package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
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
	e.sourceIntelligence = mcptest.NewBackendWithInstructions(t, "source-intelligence", "Search before you read.",
		"source_search", "source_get", "symbol_search", "repository_search")
	e.artifact = mcptest.NewBackendWithInstructions(t, "artifact", "Publish with a title.",
		"artifact_search", "artifact_get", "artifact_publish")

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
	e.registerWithAuth(name, url, nil)
}

func (e *env) registerWithAuth(name, url string, auth *gatev1alpha1.BackendAuth) {
	e.t.Helper()
	e.servers = append(e.servers, gatev1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPServerSpec{Endpoint: url, Auth: auth},
	})
	e.registry.Ensure(name, url, auth)
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
	e.registry.Ensure("artifact", revived.URL, nil)
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

// MCP 2026-07-28 is the only version spoken. A client of the session-based
// protocol does not get as far as a tool list.
func TestTheSessionBasedProtocolIsRefused(t *testing.T) {
	e := newEnv(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := mcp.NewClient(&mcp.Implementation{Name: "old-agent", Version: "test"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: e.url, HTTPClient: &http.Client{Transport: bearer("token-reviewer")}},
		&mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	require.Error(t, err)
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

func TestSpeaksTheStatelessProtocol(t *testing.T) {
	e := newEnv(t, Options{})
	session := e.connect("token-reviewer")

	assert.Equal(t, ProtocolVersion, session.InitializeResult().ProtocolVersion)
	assert.Empty(t, session.ID(), "there is no session to identify")
	// The instructions arrive in server/discover.
	assert.Contains(t, session.InitializeResult().Instructions, "Publish with a title.")
	assert.Equal(t, []string{"artifact_get", "artifact_publish", "artifact_search"}, toolNames(t, session))
	res, err := call(session, "artifact_publish")
	require.NoError(t, err)
	assert.Equal(t, `artifact/artifact_publish {"q":"x"}`, text(res))
	_, err = call(session, "source_search")
	assert.Error(t, err)
}

// roundRobin spreads requests over handlers in turn, the way a Service spreads
// them over replicas.
type roundRobin struct {
	next     atomic.Int64
	handlers []http.Handler
}

func (rr *roundRobin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rr.handlers[int(rr.next.Add(1))%len(rr.handlers)].ServeHTTP(w, r)
}

// Two replicas behind one address: each request lands on whichever is next,
// nothing pins a client to either, and a grant change reaches the client over
// its listen stream whichever replica holds it.
func TestAnyReplicaAnswersAnyRequest(t *testing.T) {
	e := newEnv(t, Options{})
	second := New(Options{Registry: e.registry, Logger: logr.Discard()})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = second.Start(ctx) }()

	var handlers []http.Handler
	for _, gw := range []*Gateway{e.gateway, second} {
		h, err := gw.Handler(HandlerOptions{Verifier: fakeVerifier})
		require.NoError(t, err)
		handlers = append(handlers, h)
	}
	srv := httptest.NewServer(&roundRobin{handlers: handlers})
	t.Cleanup(srv.Close)
	e.url = srv.URL + "/mcp"
	apply := func() {
		snapshot := policy.NewSnapshot(e.servers, e.toolSets, e.capabilities, e.grants)
		e.gateway.SetPolicy(snapshot)
		second.SetPolicy(snapshot)
	}
	apply()

	changed := make(chan struct{}, 8)
	session, err := e.connectWith("token-nobody", &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} },
	})
	require.NoError(t, err)
	for range 4 {
		assert.Empty(t, toolNames(t, session))
	}

	e.grants = append(e.grants, grant("nobody", "nobody", "artifact-authoring"))
	apply()
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the listen stream was not told that the tools changed")
	}
	for range 4 {
		assert.Equal(t, []string{"artifact_get", "artifact_publish", "artifact_search"}, toolNames(t, session))
		res, err := call(session, "artifact_get")
		require.NoError(t, err)
		assert.False(t, res.IsError)
	}
}

// A tool set with patterns exposes what the catalog matches, minus what it
// excludes — and the exclusion holds per call, not just in the list.
func TestPatterns_ExposeWhatTheCatalogMatches(t *testing.T) {
	e := newEnv(t, Options{})
	ts := toolSet("source-patterns", "source-intelligence", "source_*", "*_search")
	ts.Spec.Exclude = []string{"repository_*"}
	e.toolSets = append(e.toolSets, ts)
	e.capabilities = append(e.capabilities, capability("patterns", "source-patterns"))
	e.grants = append(e.grants, grant("nobody", "nobody", "patterns"))
	e.apply()

	session := e.connect("token-nobody")
	assert.Equal(t, []string{"source_get", "source_search", "symbol_search"}, toolNames(t, session))

	res, err := call(session, "symbol_search")
	require.NoError(t, err)
	assert.Equal(t, `source-intelligence/symbol_search {"q":"x"}`, text(res))
	_, err = call(session, "repository_search")
	assert.Error(t, err, "matched by a pattern, removed by exclude, refused")
}

// A pattern grants what the backend offers now: a tool the backend adds later
// reaches an open session, with a list_changed notification, without any
// manifest changing.
func TestPatterns_AToolTheBackendAddsReachesOpenSessions(t *testing.T) {
	e := newEnv(t, Options{})
	e.toolSets = []gatev1alpha1.MCPToolSet{toolSet("artifact-all", "artifact", "artifact_*")}
	e.capabilities = []gatev1alpha1.MCPCapability{capability("artifact-all", "artifact-all")}
	e.grants = []gatev1alpha1.MCPGrant{grant("reviewer", "reviewer", "artifact-all")}
	e.apply()

	changed := make(chan struct{}, 8)
	session, err := e.connectWith("token-reviewer", &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) { changed <- struct{}{} },
	})
	require.NoError(t, err)
	require.Equal(t, []string{"artifact_get", "artifact_publish", "artifact_search"}, toolNames(t, session))

	e.artifact.AddTool("artifact_delete")
	_, updated, err := e.registry.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	require.True(t, updated)
	e.gateway.Invalidate()

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the session was not told that its tools changed")
	}
	assert.Contains(t, toolNames(t, session), "artifact_delete")
	res, err := call(session, "artifact_delete")
	require.NoError(t, err)
	assert.Equal(t, `artifact/artifact_delete {"q":"x"}`, text(res))
}

// Two backends whose patterns match the same name: neither is exposed under it.
func TestPatterns_AConflictTheyCauseExposesNeither(t *testing.T) {
	e := newEnv(t, Options{})
	engram := mcptest.NewBackend(t, "engram", "remember", "artifact_get")
	e.register("engram", engram.URL)
	e.toolSets = []gatev1alpha1.MCPToolSet{
		toolSet("artifact-all", "artifact", "*"),
		toolSet("engram-all", "engram", "*"),
	}
	e.capabilities = []gatev1alpha1.MCPCapability{capability("everything", "artifact-all", "engram-all")}
	e.grants = []gatev1alpha1.MCPGrant{grant("reviewer", "reviewer", "everything")}
	e.apply()

	names := toolNames(t, e.connect("token-reviewer"))
	assert.Equal(t, []string{"artifact_publish", "artifact_search", "remember"}, names)
}

// Agents get the backends' instructions, and only those of the backends their
// grants reach: a subject holding nothing of source-intelligence learns nothing
// about it.
func TestInstructions_FollowTheGrants(t *testing.T) {
	e := newEnv(t, Options{})

	assert.Equal(t,
		"## artifact\n\nTools from this server: artifact_get, artifact_search\n\nPublish with a title.\n\n"+
			"## source-intelligence\n\nTools from this server: source_get, source_search, symbol_search\n\nSearch before you read.",
		e.connect("token-coding-agent").InitializeResult().Instructions)

	reviewer := e.connect("token-reviewer").InitializeResult().Instructions
	assert.Equal(t, "## artifact\n\nTools from this server: artifact_get, artifact_publish, artifact_search\n\nPublish with a title.", reviewer)
	assert.NotContains(t, reviewer, "source-intelligence")

	assert.Empty(t, e.connect("token-nobody").InitializeResult().Instructions)
}

func TestInstructions_GatewayPrefaceComesFirst(t *testing.T) {
	e := newEnv(t, Options{Instructions: "Tools reach several backends through mcp-gate."})
	instructions := e.connect("token-reviewer").InitializeResult().Instructions
	assert.True(t, strings.HasPrefix(instructions, "Tools reach several backends through mcp-gate.\n\n## artifact\n\n"), instructions)
}

// A backend redeployed with new instructions and the same tools has changed:
// the revision moves, and sessions started afterwards get the new text.
func TestInstructions_ChangedInstructionsReachTheNextDiscover(t *testing.T) {
	e := newEnv(t, Options{})
	before, _, _ := e.registry.Catalog("artifact")

	e.artifact.SetInstructions("Publish with a title and tags.")
	e.artifact.Restart("artifact_search", "artifact_get", "artifact_publish")
	after, changed, err := e.registry.Discover(context.Background(), "artifact")
	require.NoError(t, err)
	assert.True(t, changed, "changed instructions are a changed catalog")
	assert.NotEqual(t, before.Revision, after.Revision)

	assert.Contains(t, e.connect("token-reviewer").InitializeResult().Instructions, "Publish with a title and tags.")
}

// A backend that is down past the catalog's max age drops out of the
// instructions along with its tools.
func TestInstructions_UnusableBackendIsLeftOut(t *testing.T) {
	e := newEnv(t, Options{CatalogMaxAge: time.Nanosecond})
	e.sourceIntelligence.Close()
	_, _, err := e.registry.Discover(context.Background(), "source-intelligence")
	require.Error(t, err)

	instructions := e.connect("token-coding-agent").InitializeResult().Instructions
	assert.NotContains(t, instructions, "source-intelligence")
	assert.Contains(t, instructions, "## artifact")
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

// Two backends that name a tool alike: without renaming neither is exposed;
// with a prefix on one of them both are, each call reaches its own backend
// under the backend's own name, and the instructions say which name is which.
func TestRenames_TwoBackendsWithTheSameToolName(t *testing.T) {
	e := newEnv(t, Options{})
	engram := mcptest.NewBackendWithInstructions(t, "engram", "Use artifact_get to recall.", "artifact_get", "remember")
	e.register("engram", engram.URL)
	e.grants = []gatev1alpha1.MCPGrant{grant("reviewer", "reviewer", "both")}

	e.toolSets = append(e.toolSets, toolSet("engram-all", "engram", "*"))
	e.capabilities = append(e.capabilities, capability("both", "artifact-read", "engram-all"))
	e.apply()
	assert.Equal(t, []string{"artifact_search", "remember"}, toolNames(t, e.connect("token-reviewer")),
		"the colliding name is exposed by neither")

	renamed := toolSet("engram-all", "engram", "*")
	renamed.Spec.Prefix = "engram_"
	renamed.Spec.Aliases = map[string]gatev1alpha1.ToolName{"remember": "memorize"}
	e.toolSets[len(e.toolSets)-1] = renamed
	e.apply()
	session := e.connect("token-reviewer")
	assert.Equal(t, []string{"artifact_get", "artifact_search", "engram_artifact_get", "memorize"}, toolNames(t, session))

	res, err := call(session, "engram_artifact_get")
	require.NoError(t, err)
	assert.Equal(t, `engram/artifact_get {"q":"x"}`, text(res))
	res, err = call(session, "memorize")
	require.NoError(t, err)
	assert.Equal(t, `engram/remember {"q":"x"}`, text(res))
	res, err = call(session, "artifact_get")
	require.NoError(t, err)
	assert.Equal(t, `artifact/artifact_get {"q":"x"}`, text(res))
	_, err = call(session, "remember")
	assert.Error(t, err, "an aliased tool is not offered under its backend name as well")

	assert.Contains(t, session.InitializeResult().Instructions,
		"Tools from this server: engram_artifact_get (artifact_get), memorize (remember)\n\nUse artifact_get to recall.")
}
