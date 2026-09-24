package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/gateway"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// ---------------------------------------------------------------- fixtures

type fakeGateway struct {
	snapshot     *policy.Snapshot
	usable       map[string]*discovery.Catalog
	instructions map[string]string
}

func (g *fakeGateway) Policy() *policy.Snapshot { return g.snapshot }
func (g *fakeGateway) UsableCatalog(server string) (*discovery.Catalog, bool) {
	c, ok := g.usable[server]
	return c, ok
}
func (g *fakeGateway) Instructions(subject string) string { return g.instructions[subject] }
func (g *fakeGateway) InstructionsOf(res policy.Resolution) string {
	return "for " + strings.Join(resolved(res), ",")
}
func (g *fakeGateway) Preview(_ context.Context, res policy.Resolution) (gateway.Preview, error) {
	list, _ := json.Marshal(map[string]any{"tools": resolved(res)})
	return gateway.Preview{Discover: json.RawMessage(`{"previewed":true}`), ToolsList: []json.RawMessage{list}}, nil
}

type fakeRegistry struct {
	catalogs map[string]*discovery.Catalog
	health   map[string]discovery.Health
}

func (r *fakeRegistry) Catalog(name string) (*discovery.Catalog, discovery.Health, bool) {
	c, ok := r.catalogs[name]
	return c, r.health[name], ok
}
func (r *fakeRegistry) Health(name string) (discovery.Health, bool) {
	h, ok := r.health[name]
	return h, ok
}

func tool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func obj(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Generation: 1, CreationTimestamp: metav1.NewTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))}
}

var observed = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// newTestServer is a small world: source-intelligence and search are up,
// tickets has been failing too long to be served; field-ops puts two tools
// named search together; agent-coder has a second grant naming a capability
// that does not exist.
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	servers := []mcpv1alpha1.MCPServer{
		{ObjectMeta: obj("source-intelligence"), Spec: mcpv1alpha1.MCPServerSpec{Endpoint: "http://si/mcp"}},
		{ObjectMeta: obj("search"), Spec: mcpv1alpha1.MCPServerSpec{Endpoint: "http://search/mcp", Auth: &mcpv1alpha1.BackendAuth{
			Type:   mcpv1alpha1.BackendAuthAPIKey,
			APIKey: &mcpv1alpha1.APIKeyAuth{SecretRef: mcpv1alpha1.SecretKeyRef{Name: "search", Key: "key"}, Header: "X-API-Key"},
		}}},
		{ObjectMeta: obj("weather"), Spec: mcpv1alpha1.MCPServerSpec{Endpoint: "http://weather/mcp"}},
		{ObjectMeta: obj("tickets"), Spec: mcpv1alpha1.MCPServerSpec{Endpoint: "http://tickets/mcp"},
			Status: mcpv1alpha1.MCPServerStatus{Conditions: []metav1.Condition{{
				Type: "Ready", Status: "False", Reason: "Unauthorized", Message: "401", LastTransitionTime: metav1.NewTime(observed),
			}}}},
	}
	toolSets := []mcpv1alpha1.MCPToolSet{
		{ObjectMeta: obj("si-analysis"), Spec: mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "source-intelligence", Tools: []string{"code.*", "repositories.*", "refs.list"}, Exclude: []string{"repositories.read_file"},
		}},
		{ObjectMeta: obj("si-search"), Spec: mcpv1alpha1.MCPToolSetSpec{ServerRef: "source-intelligence", Tools: []string{"code.search"}}},
		{ObjectMeta: obj("web-search"), Spec: mcpv1alpha1.MCPToolSetSpec{ServerRef: "search", Tools: []string{"search", "fetch_page"}}},
		{ObjectMeta: obj("weather-basic"), Spec: mcpv1alpha1.MCPToolSetSpec{ServerRef: "weather", Tools: []string{"*"}}},
		{ObjectMeta: obj("ticket-read"), Spec: mcpv1alpha1.MCPToolSetSpec{ServerRef: "tickets", Tools: []string{"tickets_list"}}},
	}
	capabilities := []mcpv1alpha1.MCPCapability{
		{ObjectMeta: obj("code-research"), Spec: mcpv1alpha1.MCPCapabilitySpec{Description: "Read code.", ToolSets: []string{"si-analysis", "si-search", "ticket-read"}}},
		{ObjectMeta: obj("field-ops"), Spec: mcpv1alpha1.MCPCapabilitySpec{ToolSets: []string{"web-search", "weather-basic", "gone"}}},
	}
	grants := []mcpv1alpha1.MCPGrant{
		{ObjectMeta: obj("coder"), Spec: mcpv1alpha1.MCPGrantSpec{Subject: "agent-coder", Capabilities: []string{"code-research"}}},
		{ObjectMeta: obj("coder-web"), Spec: mcpv1alpha1.MCPGrantSpec{Subject: "agent-coder", Capabilities: []string{"web-research"}}},
		{ObjectMeta: obj("support"), Spec: mcpv1alpha1.MCPGrantSpec{Subject: "https://idp.example.com/clients/svc-support", Capabilities: []string{"field-ops"}}},
	}

	si := discovery.NewCatalog([]*mcp.Tool{
		tool("code.search", "Search the source."), tool("code.symbol", "Look up a declaration."),
		tool("repositories.list", "List repositories."), tool("repositories.read_file", "Read a file."),
		tool("refs.list", "List branches."), tool("admin.reindex", "Reindex."),
	}, "Treat a partial answer as incomplete.")
	search := discovery.NewCatalog([]*mcp.Tool{tool("search", "Search the web."), tool("fetch", "Fetch a page.")}, "")
	weather := discovery.NewCatalog([]*mcp.Tool{tool("search", "Find a location."), tool("get_forecast", "Forecast.")}, "Resolve a place first.")
	weather.Handshake = &mcp.InitializeResult{
		ProtocolVersion: "2026-07-28",
		ServerInfo:      &mcp.Implementation{Name: "weather", Version: "1"},
		Capabilities:    &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	}
	weather.Pages = []*mcp.ListToolsResult{{Tools: []*mcp.Tool{tool("get_forecast", "Forecast.")}}}
	tickets := discovery.NewCatalog([]*mcp.Tool{tool("tickets_list", "List tickets.")}, "")

	scheme := runtime.NewScheme()
	require.NoError(t, mcpv1alpha1.AddToScheme(scheme))
	var objs []client.Object
	for i := range servers {
		objs = append(objs, &servers[i])
	}
	for i := range toolSets {
		objs = append(objs, &toolSets[i])
	}
	for i := range capabilities {
		objs = append(objs, &capabilities[i])
	}
	for i := range grants {
		objs = append(objs, &grants[i])
	}

	s := &Server{
		Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		Gateway: &fakeGateway{
			snapshot:     policy.NewSnapshot(servers, toolSets, capabilities, grants),
			usable:       map[string]*discovery.Catalog{"source-intelligence": si, "search": search, "weather": weather},
			instructions: map[string]string{"agent-coder": "## source-intelligence\n\nTools from this server: code.search"},
		},
		Registry: &fakeRegistry{
			catalogs: map[string]*discovery.Catalog{"source-intelligence": si, "search": search, "weather": weather, "tickets": tickets},
			health: map[string]discovery.Health{
				"source-intelligence": {LastSuccess: observed},
				"search":              {LastSuccess: observed},
				"weather":             {LastSuccess: observed},
				"tickets":             {LastSuccess: observed.Add(-3 * time.Hour), Failures: 40, LastError: assert.AnError},
			},
		},
		Replica: "mcp-gateway-0",
		Now:     func() time.Time { return observed },
	}
	pass := func(h http.Handler) http.Handler { return h }
	return Handler(s, pass, logr.Discard())
}

func get[T any](t *testing.T, h http.Handler, path string, wantStatus int) T {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, BasePath+path, nil))
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	var out T
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(&out), rec.Body.String())
	return out
}

// ---------------------------------------------------------------- tests

func TestServers(t *testing.T) {
	h := newTestServer(t)

	list := get[api.ServerList](t, h, "/servers", http.StatusOK)
	require.Len(t, list.Items, 4)
	assert.Equal(t, "search", list.Items[0].Meta.Name, "sorted by name")
	assert.Equal(t, api.Source{Replica: "mcp-gateway-0", ObservedAt: observed}, list.Source)

	si := get[api.Server](t, h, "/servers/source-intelligence", http.StatusOK)
	assert.Equal(t, "1m0s", si.Spec.DiscoveryInterval, "defaulted")
	assert.Equal(t, api.None, si.Spec.Auth.Type, "defaulted")
	assert.True(t, si.Discovery.Usable)
	require.NotNil(t, si.Catalog)
	assert.Equal(t, "Treat a partial answer as incomplete.", si.Catalog.Instructions)
	require.Len(t, si.Catalog.Tools, 6)
	assert.Equal(t, "admin.reindex", si.Catalog.Tools[0].Name)
	assert.Equal(t, "Reindex.", *si.Catalog.Tools[0].Description)
	assert.Equal(t, "object", si.Catalog.Tools[0].InputSchema["type"])
	assert.True(t, *si.Catalog.Tools[0].Annotations.ReadOnlyHint)
	assert.Equal(t, []string{"si-analysis", "si-search"}, si.UsedBy)

	search := get[api.Server](t, h, "/servers/search", http.StatusOK)
	assert.Equal(t, api.APIKey, search.Spec.Auth.Type)
	assert.Equal(t, api.SecretKeyRef{Name: "search", Key: "key"}, search.Spec.Auth.ApiKey.SecretRef)

	tickets := get[api.Server](t, h, "/servers/tickets", http.StatusOK)
	assert.False(t, tickets.Discovery.Usable, "failing for longer than the max age")
	assert.True(t, tickets.Discovery.Discovered)
	assert.Equal(t, 40, tickets.Discovery.ConsecutiveFailures)
	assert.Equal(t, assert.AnError.Error(), *tickets.Discovery.LastError)
	require.NotNil(t, tickets.Catalog, "the last catalog read is shown, however old")
	assert.Equal(t, "Unauthorized", tickets.Status.Conditions[0].Reason)

	get[api.Problem](t, h, "/servers/nope", http.StatusNotFound)
}

func TestToolSetExplanation(t *testing.T) {
	h := newTestServer(t)

	ts := get[api.ToolSet](t, h, "/toolsets/si-analysis", http.StatusOK)
	assert.Equal(t, []string{"code-research"}, ts.UsedBy)
	sel := ts.Selection
	assert.True(t, sel.CatalogAvailable)

	states := map[string]api.CatalogToolSelectionState{}
	for _, tool := range sel.Tools {
		states[tool.Tool.Name] = tool.State
	}
	assert.Equal(t, map[string]api.CatalogToolSelectionState{
		"admin.reindex":          api.CatalogToolSelectionStateUnselected,
		"code.search":            api.CatalogToolSelectionStateSelected,
		"code.symbol":            api.CatalogToolSelectionStateSelected,
		"refs.list":              api.CatalogToolSelectionStateSelected,
		"repositories.list":      api.CatalogToolSelectionStateSelected,
		"repositories.read_file": api.CatalogToolSelectionStateExcluded,
	}, states)
	for _, tool := range sel.Tools {
		if tool.Tool.Name == "repositories.read_file" {
			assert.Equal(t, "Read a file.", *tool.Tool.Description, "definitions come along")
			assert.Equal(t, []string{"repositories.*"}, tool.SelectedBy)
			assert.Equal(t, []string{"repositories.read_file"}, tool.ExcludedBy)
		}
	}
	assert.Equal(t, api.SelectionEntry{Entry: "code.*", Kind: api.SelectionEntryKindPattern, Outcome: api.SelectionEntryOutcomeOk,
		Selects: []string{"code.search", "code.symbol"}}, sel.Entries[0])
	assert.Equal(t, []api.ExclusionEntry{{Entry: "repositories.read_file", Kind: api.ExclusionEntryKindName, Removes: []string{"repositories.read_file"}}}, sel.Exclusions)

	missing := get[api.ToolSet](t, h, "/toolsets/web-search", http.StatusOK)
	assert.Equal(t, []string{"fetch_page"}, missing.Selection.Missing)
	assert.Equal(t, api.SelectionEntryOutcomeMissing, missing.Selection.Entries[1].Outcome)

	down := get[api.ToolSet](t, h, "/toolsets/ticket-read", http.StatusOK)
	assert.False(t, down.Selection.CatalogAvailable)
	assert.Empty(t, down.Selection.Tools)
}

func TestCapability(t *testing.T) {
	h := newTestServer(t)

	c := get[api.Capability](t, h, "/capabilities/code-research", http.StatusOK)
	assert.Equal(t, []string{"coder"}, c.GrantedBy)
	byName := map[string]api.ResolvedTool{}
	for _, tool := range c.Tools {
		byName[tool.Name] = tool
	}
	assert.Equal(t, []string{"si-analysis", "si-search"}, byName["code.search"].ToolSets, "one tool through two sets")
	assert.True(t, byName["code.search"].Available)
	assert.False(t, byName["tickets_list"].Available, "claimed, but the catalog is not served")
	assert.Empty(t, c.Conflicts)

	f := get[api.Capability](t, h, "/capabilities/field-ops", http.StatusOK)
	assert.Equal(t, []string{"gone"}, f.MissingToolSets)
	require.Len(t, f.Conflicts, 1)
	assert.Equal(t, "search", f.Conflicts[0].Name)
	assert.Equal(t, []api.ResolvedTool{
		{Name: "search", Ref: api.ToolRef{Server: "search", Tool: "search"}, ToolSets: []string{"web-search"}},
		{Name: "search", Ref: api.ToolRef{Server: "weather", Tool: "search"}, ToolSets: []string{"weather-basic"}},
	}, f.Conflicts[0].Claimants)
}

func TestGrant(t *testing.T) {
	h := newTestServer(t)

	g := get[api.Grant](t, h, "/grants/coder-web", http.StatusOK)
	assert.Equal(t, []string{"web-research"}, g.MissingCapabilities)
	assert.Equal(t, []string{"coder"}, g.SameSubject)

	list := get[api.GrantList](t, h, "/grants", http.StatusOK)
	assert.Len(t, list.Items, 3)
}

func TestSubject(t *testing.T) {
	h := newTestServer(t)

	list := get[api.SubjectList](t, h, "/subjects", http.StatusOK)
	require.Len(t, list.Items, 2)
	assert.Equal(t, api.SubjectSummary{Subject: "agent-coder", Grants: []string{"coder", "coder-web"}, ToolCount: 4}, list.Items[0])

	s := get[api.Subject](t, h, "/subject?subject=agent-coder", http.StatusOK)
	assert.Equal(t, []string{"web-research"}, s.MissingCapabilities)
	assert.Contains(t, s.Instructions, "## source-intelligence")
	byName := map[string]api.SubjectTool{}
	for _, tool := range s.Tools {
		byName[tool.Name] = tool
	}
	search := byName["code.search"]
	assert.Equal(t, api.SubjectToolStateOffered, search.State)
	require.Len(t, search.Claimants, 1)
	assert.Equal(t, "Search the source.", *search.Claimants[0].Definition.Description)
	assert.Equal(t, []api.GrantPath{
		{Grant: "coder", Capability: "code-research", ToolSet: "si-analysis"},
		{Grant: "coder", Capability: "code-research", ToolSet: "si-search"},
	}, search.Claimants[0].Paths)
	assert.Equal(t, api.SubjectToolStateUnavailable, byName["tickets_list"].State)
	assert.Nil(t, byName["tickets_list"].Claimants[0].Definition)

	// A subject that is a URL travels as a query parameter.
	support := get[api.Subject](t, h, "/subject?subject="+url.QueryEscape("https://idp.example.com/clients/svc-support"), http.StatusOK)
	var conflict api.SubjectTool
	for _, tool := range support.Tools {
		if tool.Name == "search" {
			conflict = tool
		}
	}
	assert.Equal(t, api.SubjectToolStateConflict, conflict.State)
	assert.Len(t, conflict.Claimants, 2)

	nobody := get[api.Subject](t, h, "/subject?subject=nobody", http.StatusOK)
	assert.Empty(t, nobody.Tools)
	assert.Empty(t, nobody.Grants)

	get[api.Problem](t, h, "/subject", http.StatusBadRequest)
}

// TestAPIMirrorsTheCRDs guards the one place the spec repeats something:
// spec and status of the four kinds, which the API passes on as written. A
// field added to a CRD and not to api/openapi/inspect.yaml fails here, since
// every field below is set and the decoding refuses what it does not know.
func TestAPIMirrorsTheCRDs(t *testing.T) {
	cc := &mcpv1alpha1.ClientCredentialsAuth{
		TokenURL: "https://idp/token", ClientID: "id", ClientIDRef: &mcpv1alpha1.SecretKeyRef{Name: "s", Key: "id"},
		ClientSecretRef: mcpv1alpha1.SecretKeyRef{Name: "s", Key: "secret"}, Scopes: []string{"a"}, Audience: []string{"b"},
	}
	key := &mcpv1alpha1.APIKeyAuth{SecretRef: mcpv1alpha1.SecretKeyRef{Name: "s", Key: "k"}, Header: "X-Key", Prefix: ptr.To("Key ")}
	now := metav1.NewTime(observed)
	cond := []metav1.Condition{{Type: "Ready", Status: "True", Reason: "Available", Message: "m", LastTransitionTime: now, ObservedGeneration: 1}}

	for _, tc := range []struct {
		name string
		in   any
		out  any
	}{
		{"MCPServer spec", mcpv1alpha1.MCPServerSpec{
			Endpoint: "http://x/mcp", DiscoveryInterval: &metav1.Duration{Duration: time.Minute},
			Auth: &mcpv1alpha1.BackendAuth{
				Type: mcpv1alpha1.BackendAuthPassthrough, Passthrough: &mcpv1alpha1.PassthroughAuth{Discovery: &mcpv1alpha1.DiscoveryAuth{ClientCredentials: cc, APIKey: key}},
				ClientCredentials: cc, APIKey: key,
			},
		}, &api.ServerSpec{}},
		{"MCPServer status", mcpv1alpha1.MCPServerStatus{
			ObservedGeneration: 1, Available: true, DiscoveredTools: 3, CatalogRevision: "r", LastDiscoveryTime: &now, Conditions: cond,
		}, &api.ServerStatus{}},
		{"MCPToolSet spec", mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "s", Tools: []string{"a"}, Exclude: []string{"b"}, Prefix: "p_", Aliases: map[string]mcpv1alpha1.ToolName{"a": "z"},
		}, &api.ToolSetSpec{}},
		{"MCPToolSet status", mcpv1alpha1.MCPToolSetStatus{
			ObservedGeneration: 1, CatalogRevision: "r", ResolvedTools: []string{"a"}, BackendNames: map[string]string{"p_a": "a"},
			ResolvedToolCount: 1, MissingTools: []string{"m"}, UnmatchedPatterns: []string{"u*"}, UnusedAliases: []string{"x"},
			ConflictingNames: []string{"c"}, Conditions: cond,
		}, &api.ToolSetStatus{}},
		{"MCPCapability spec", mcpv1alpha1.MCPCapabilitySpec{Description: "d", ToolSets: []string{"t"}}, &api.CapabilitySpec{}},
		{"MCPCapability status", mcpv1alpha1.MCPCapabilityStatus{ObservedGeneration: 1, Conditions: cond}, &api.CapabilityStatus{}},
		{"MCPGrant spec", mcpv1alpha1.MCPGrantSpec{Subject: "s", Capabilities: []string{"c"}}, &api.GrantSpec{}},
		{"MCPGrant status", mcpv1alpha1.MCPGrantStatus{ObservedGeneration: 1, Conditions: cond}, &api.GrantStatus{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.in)
			require.NoError(t, err)
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.DisallowUnknownFields()
			require.NoError(t, dec.Decode(tc.out), "the API is missing a field of %s", tc.name)
			back, err := json.Marshal(tc.out)
			require.NoError(t, err)
			assert.JSONEq(t, string(b), string(back), "a field of %s is lost or renamed on the way", tc.name)
		})
	}
}


// resolved lists the tools a resolution comes to, sorted, for the fake
// Preview to report back.
func resolved(res policy.Resolution) []string {
	var out []string
	for name := range res.Tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func TestMCPViews(t *testing.T) {
	h := newTestServer(t)
	agentTools := func(ex api.AgentExchange) []string {
		var got struct {
			Tools []string `json:"tools"`
		}
		require.NoError(t, json.Unmarshal(ex.ToolsList[0], &got))
		return got.Tools
	}

	// The agent's side is the gateway's own answer; what matters here is
	// which resolution each resource hands it.
	ts := get[api.AgentExchange](t, h, "/toolsets/si-analysis/mcp", http.StatusOK)
	assert.Equal(t, []string{"code.search", "code.symbol", "refs.list", "repositories.list"}, agentTools(ts))
	assert.JSONEq(t, `{"previewed":true}`, string(ts.Discover))

	c := get[api.AgentExchange](t, h, "/capabilities/field-ops/mcp", http.StatusOK)
	assert.Equal(t, []string{"fetch_page", "get_forecast"}, agentTools(c), "the resolution, before the catalog check the gateway makes")

	g := get[api.AgentExchange](t, h, "/grants/coder/mcp", http.StatusOK)
	assert.Contains(t, agentTools(g), "tickets_list")

	sub := get[api.AgentExchange](t, h, "/subject/mcp?subject=agent-coder", http.StatusOK)
	assert.Equal(t, agentTools(g), agentTools(sub), "coder-web adds nothing")

	srv := get[api.BackendExchange](t, h, "/servers/weather/mcp", http.StatusOK)
	require.NotNil(t, srv.Handshake)
	assert.JSONEq(t, `{"capabilities":{"tools":{}},"protocolVersion":"2026-07-28","serverInfo":{"name":"weather","version":"1"}}`, string(*srv.Handshake))
	require.Len(t, srv.ToolsList, 1)
	assert.Contains(t, string(srv.ToolsList[0]), `"name":"get_forecast"`)

	never := get[api.BackendExchange](t, h, "/servers/search/mcp", http.StatusOK)
	assert.Nil(t, never.Handshake, "a catalog without a handshake read")

	for _, path := range []string{"/servers/nope/mcp", "/toolsets/nope/mcp", "/capabilities/nope/mcp", "/grants/nope/mcp"} {
		get[api.Problem](t, h, path, http.StatusNotFound)
	}
}

func TestInstructionsPerResource(t *testing.T) {
	h := newTestServer(t)
	ts := get[api.ToolSet](t, h, "/toolsets/si-search", http.StatusOK)
	assert.Equal(t, "for code.search", ts.Instructions, "what the tool set alone resolves to")
	c := get[api.Capability](t, h, "/capabilities/field-ops", http.StatusOK)
	assert.Equal(t, "for fetch_page,get_forecast", c.Instructions)
	g := get[api.Grant](t, h, "/grants/coder-web", http.StatusOK)
	assert.Equal(t, "for ", g.Instructions, "a grant of nothing")
}
