// Package inspect serves the inspect API: a read-only view of what one gateway
// replica knows, for the console.
//
// The API is defined in api/openapi/inspect.yaml, and package api is generated
// from it. Server implements the generated interface; a change to the API is a
// change to the spec first, and this package does not compile until it follows.
//
// Everything here is read from what the gateway holds anyway — the informer
// cache of the four kinds, the discovery registry, the policy snapshot — and
// resolved through internal/policy, so what the API says a subject gets is what
// the gateway enforces.
package inspect

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/gateway"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// Gateway is what the API reads from the running gateway.
type Gateway interface {
	Policy() *policy.Snapshot
	UsableCatalog(server string) (*discovery.Catalog, bool)
	Instructions(subject string) string
	InstructionsOf(res policy.Resolution) string
	// Preview answers server/discover and tools/list as the gateway does
	// for an agent offered res.
	Preview(ctx context.Context, res policy.Resolution) (gateway.Preview, error)
}

// Registry is what the API reads from the discovery registry.
type Registry interface {
	Catalog(name string) (*discovery.Catalog, discovery.Health, bool)
	Health(name string) (discovery.Health, bool)
}

// Server implements the inspect API.
type Server struct {
	// Reader lists the four kinds; the manager's cached client.
	Reader   client.Reader
	Gateway  Gateway
	Registry Registry
	// Replica names this gateway replica in every answer.
	Replica string
	// Events serves /events; nil answers it with 503.
	Events *Events
	// Now defaults to time.Now.
	Now func() time.Time
}

var _ api.StrictServerInterface = (*Server)(nil)

func (s *Server) source() api.Source {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	return api.Source{Replica: s.Replica, ObservedAt: now().UTC()}
}

// catalogs expands patterns against what the gateway serves, as it does itself.
func (s *Server) catalogs() policy.Catalogs {
	return func(server string) ([]string, bool) {
		c, ok := s.Gateway.UsableCatalog(server)
		if !ok {
			return nil, false
		}
		return c.Names(), true
	}
}

// offered reports whether the gateway serves a tool right now, and its
// definition if so.
func (s *Server) offered(ref policy.ToolRef) (*api.ToolDefinition, bool) {
	c, ok := s.Gateway.UsableCatalog(ref.Server)
	if !ok {
		return nil, false
	}
	t, ok := c.Tool(ref.Tool)
	if !ok {
		return nil, false
	}
	def := toolDefinition(t)
	return &def, true
}

// world is the four kinds as listed at one moment, each sorted by name.
type world struct {
	servers      []mcpv1alpha1.MCPServer
	toolSets     []mcpv1alpha1.MCPToolSet
	capabilities []mcpv1alpha1.MCPCapability
	grants       []mcpv1alpha1.MCPGrant
}

func (s *Server) load(ctx context.Context) (*world, error) {
	var servers mcpv1alpha1.MCPServerList
	var toolSets mcpv1alpha1.MCPToolSetList
	var capabilities mcpv1alpha1.MCPCapabilityList
	var grants mcpv1alpha1.MCPGrantList
	for _, l := range []client.ObjectList{&servers, &toolSets, &capabilities, &grants} {
		if err := s.Reader.List(ctx, l); err != nil {
			return nil, err
		}
	}
	w := &world{servers.Items, toolSets.Items, capabilities.Items, grants.Items}
	sort.Slice(w.servers, func(i, j int) bool { return w.servers[i].Name < w.servers[j].Name })
	sort.Slice(w.toolSets, func(i, j int) bool { return w.toolSets[i].Name < w.toolSets[j].Name })
	sort.Slice(w.capabilities, func(i, j int) bool { return w.capabilities[i].Name < w.capabilities[j].Name })
	sort.Slice(w.grants, func(i, j int) bool { return w.grants[i].Name < w.grants[j].Name })
	return w, nil
}

func (w *world) server(name string) (*mcpv1alpha1.MCPServer, bool) {
	for i := range w.servers {
		if w.servers[i].Name == name {
			return &w.servers[i], true
		}
	}
	return nil, false
}

func (w *world) toolSet(name string) (*mcpv1alpha1.MCPToolSet, bool) {
	for i := range w.toolSets {
		if w.toolSets[i].Name == name {
			return &w.toolSets[i], true
		}
	}
	return nil, false
}

func (w *world) capability(name string) (*mcpv1alpha1.MCPCapability, bool) {
	for i := range w.capabilities {
		if w.capabilities[i].Name == name {
			return &w.capabilities[i], true
		}
	}
	return nil, false
}

func (w *world) grant(name string) (*mcpv1alpha1.MCPGrant, bool) {
	for i := range w.grants {
		if w.grants[i].Name == name {
			return &w.grants[i], true
		}
	}
	return nil, false
}

func notFound(kind, name string) api.NotFoundApplicationProblemPlusJSONResponse {
	detail := "no " + kind + " named " + name
	return api.NotFoundApplicationProblemPlusJSONResponse{Title: "Not Found", Status: 404, Detail: &detail}
}

// isInvalidParam turns a parameter the client got wrong into the problem a
// 400 answers with.
func isInvalidParam(err error) (api.BadRequestApplicationProblemPlusJSONResponse, bool) {
	var bad invalidParam
	if !errors.As(err, &bad) {
		return api.BadRequestApplicationProblemPlusJSONResponse{}, false
	}
	return api.BadRequestApplicationProblemPlusJSONResponse{Title: "Bad Request", Status: 400, Detail: &bad.msg}, true
}

// ---------------------------------------------------------------- events

func (s *Server) WatchEvents(ctx context.Context, _ api.WatchEventsRequestObject) (api.WatchEventsResponseObject, error) {
	unavailable := func(detail string) api.WatchEventsResponseObject {
		return api.WatchEvents503ApplicationProblemPlusJSONResponse{Title: "Service Unavailable", Status: 503, Detail: &detail}
	}
	if s.Events == nil {
		return unavailable("this replica does not stream events"), nil
	}
	body, err := s.Events.stream(ctx)
	if errors.Is(err, errTooManyStreams) {
		return unavailable("this replica streams to as many readers as it allows; poll instead"), nil
	}
	if err != nil {
		return nil, err
	}
	return api.WatchEvents200TexteventStreamResponse{Body: body}, nil
}

// ---------------------------------------------------------------- servers

func (s *Server) ListServers(ctx context.Context, req api.ListServersRequestObject) (api.ListServersResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]api.ServerSummary, 0, len(w.servers))
	for i := range w.servers {
		sum, err := s.serverSummary(&w.servers[i])
		if err != nil {
			return nil, err
		}
		items = append(items, sum)
	}
	p := req.Params
	items, total, next, err := page(items, serverListing, newPageRequest(p.Limit, p.Cursor, p.Q, p.Sort))
	if bad, ok := isInvalidParam(err); ok {
		return api.ListServers400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: bad}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListServers200JSONResponse{Items: items, Total: total, NextCursor: next, Source: s.source()}, nil
}

func (s *Server) GetServer(ctx context.Context, req api.GetServerRequestObject) (api.GetServerResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	srv, ok := w.server(req.Name)
	if !ok {
		return api.GetServer404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPServer", req.Name)}, nil
	}
	sum, err := s.serverSummary(srv)
	if err != nil {
		return nil, err
	}
	out := api.Server{
		Meta: sum.Meta, Spec: sum.Spec, Status: sum.Status, Discovery: sum.Discovery,
		UsedBy: []string{},
		Source: s.source(),
	}
	if c, _, ok := s.Registry.Catalog(srv.Name); ok {
		out.Catalog = catalog(c)
	}
	for _, ts := range w.toolSets {
		if ts.Spec.ServerRef == srv.Name {
			out.UsedBy = append(out.UsedBy, ts.Name)
		}
	}
	return api.GetServer200JSONResponse(out), nil
}

func (s *Server) serverSummary(srv *mcpv1alpha1.MCPServer) (api.ServerSummary, error) {
	spec, err := serverSpec(srv)
	if err != nil {
		return api.ServerSummary{}, err
	}
	status, err := serverStatus(srv)
	if err != nil {
		return api.ServerSummary{}, err
	}
	return api.ServerSummary{Meta: meta(srv), Spec: spec, Status: status, Discovery: s.discovery(srv.Name)}, nil
}

func (s *Server) discovery(name string) api.Discovery {
	d := api.Discovery{}
	health, ok := s.Registry.Health(name)
	if !ok {
		return d
	}
	d.ConsecutiveFailures = health.Failures
	if !health.LastSuccess.IsZero() {
		t := health.LastSuccess.UTC()
		d.LastSuccess = &t
	}
	if health.Failures > 0 && health.LastError != nil {
		msg := health.LastError.Error()
		d.LastError = &msg
	}
	if c, _, ok := s.Registry.Catalog(name); ok {
		d.Discovered = true
		d.CatalogRevision = &c.Revision
		n := c.Len()
		d.ToolCount = &n
	}
	_, d.Usable = s.Gateway.UsableCatalog(name)
	return d
}

// ---------------------------------------------------------------- tool sets

func (s *Server) ListToolSets(ctx context.Context, req api.ListToolSetsRequestObject) (api.ListToolSetsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]api.ToolSetSummary, 0, len(w.toolSets))
	for i := range w.toolSets {
		sum, err := toolSetSummary(&w.toolSets[i])
		if err != nil {
			return nil, err
		}
		items = append(items, sum)
	}
	p := req.Params
	items, total, next, err := page(items, toolSetListing, newPageRequest(p.Limit, p.Cursor, p.Q, p.Sort))
	if bad, ok := isInvalidParam(err); ok {
		return api.ListToolSets400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: bad}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListToolSets200JSONResponse{Items: items, Total: total, NextCursor: next, Source: s.source()}, nil
}

func (s *Server) GetToolSet(ctx context.Context, req api.GetToolSetRequestObject) (api.GetToolSetResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	ts, ok := w.toolSet(req.Name)
	if !ok {
		return api.GetToolSet404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPToolSet", req.Name)}, nil
	}
	sum, err := toolSetSummary(ts)
	if err != nil {
		return nil, err
	}
	out := api.ToolSet{
		Meta: sum.Meta, Spec: sum.Spec, Status: sum.Status,
		UsedBy: []string{},
		Source: s.source(),
	}
	for _, c := range w.capabilities {
		if slices.Contains(c.Spec.ToolSets, ts.Name) {
			out.UsedBy = append(out.UsedBy, c.Name)
		}
	}

	var names []string
	c, usable := s.Gateway.UsableCatalog(ts.Spec.ServerRef)
	if usable {
		names = c.Names()
	}
	// Without a usable catalog Explain lists no tools, and nothing asks for a
	// definition.
	out.Selection = explanation(policy.Explain(ts.Spec, names), func(tool string) api.ToolDefinition {
		t, _ := c.Tool(tool)
		return toolDefinition(t)
	})
	out.Selection.CatalogAvailable = usable
	out.Instructions = s.Gateway.InstructionsOf(s.Gateway.Policy().ResolveToolSets([]string{ts.Name}, s.catalogs()))
	if usable {
		out.Selection.CatalogRevision = &c.Revision
	}
	return api.GetToolSet200JSONResponse(out), nil
}

// ---------------------------------------------------------------- capabilities

func (s *Server) ListCapabilities(ctx context.Context, req api.ListCapabilitiesRequestObject) (api.ListCapabilitiesResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]api.CapabilitySummary, 0, len(w.capabilities))
	for i := range w.capabilities {
		items = append(items, capabilitySummary(&w.capabilities[i]))
	}
	p := req.Params
	items, total, next, err := page(items, capabilityListing, newPageRequest(p.Limit, p.Cursor, p.Q, p.Sort))
	if bad, ok := isInvalidParam(err); ok {
		return api.ListCapabilities400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: bad}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListCapabilities200JSONResponse{Items: items, Total: total, NextCursor: next, Source: s.source()}, nil
}

func (s *Server) GetCapability(ctx context.Context, req api.GetCapabilityRequestObject) (api.GetCapabilityResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	c, ok := w.capability(req.Name)
	if !ok {
		return api.GetCapability404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPCapability", req.Name)}, nil
	}
	sum := capabilitySummary(c)
	out := api.Capability{
		Meta: sum.Meta, Spec: sum.Spec, Status: sum.Status,
		GrantedBy: []string{},
		Tools:     []api.ResolvedTool{},
		Conflicts: []api.NameConflict{},
		Source:    s.source(),
	}
	for _, g := range w.grants {
		if slices.Contains(g.Spec.Capabilities, c.Name) {
			out.GrantedBy = append(out.GrantedBy, g.Name)
		}
	}

	catalogs := s.catalogs()
	res := s.Gateway.Policy().ResolveCapabilities([]string{c.Name}, catalogs)
	out.MissingToolSets = nonNil(res.MissingToolSets)
	out.Instructions = s.Gateway.InstructionsOf(res)
	via := s.claimedThrough(w, c.Spec.ToolSets, catalogs)
	resolved := func(name string, ref policy.ToolRef) api.ResolvedTool {
		_, available := s.offered(ref)
		return api.ResolvedTool{
			Name:      name,
			Ref:       toolRef(ref),
			ToolSets:  nonNil(via[claimKey{name, ref}]),
			Available: available,
		}
	}
	for _, name := range sortedKeys(res.Tools) {
		out.Tools = append(out.Tools, resolved(name, res.Tools[name]))
	}
	for _, name := range res.ConflictNames() {
		nc := api.NameConflict{Name: name}
		for _, ref := range res.Conflicts[name] {
			rt := resolved(name, ref)
			rt.Available = false
			nc.Claimants = append(nc.Claimants, rt)
		}
		out.Conflicts = append(out.Conflicts, nc)
	}
	return api.GetCapability200JSONResponse(out), nil
}

type claimKey struct {
	name string
	ref  policy.ToolRef
}

// claimedThrough maps every (name, backend tool) the named tool sets claim to
// the tool sets that claim it, in the order given.
func (s *Server) claimedThrough(w *world, toolSets []string, catalogs policy.Catalogs) map[claimKey][]string {
	out := map[claimKey][]string{}
	for _, name := range toolSets {
		ts, ok := w.toolSet(name)
		if !ok {
			continue
		}
		if _, ok := w.server(ts.Spec.ServerRef); !ok {
			continue
		}
		names, _ := catalogs(ts.Spec.ServerRef)
		for _, c := range policy.Claims(ts.Spec, names) {
			k := claimKey{c.Exposed, policy.ToolRef{Server: ts.Spec.ServerRef, Tool: c.Tool}}
			if !slices.Contains(out[k], name) {
				out[k] = append(out[k], name)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- grants

func (s *Server) ListGrants(ctx context.Context, req api.ListGrantsRequestObject) (api.ListGrantsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]api.GrantSummary, 0, len(w.grants))
	for i := range w.grants {
		items = append(items, grantSummary(&w.grants[i]))
	}
	p := req.Params
	items, total, next, err := page(items, grantListing, newPageRequest(p.Limit, p.Cursor, p.Q, p.Sort))
	if bad, ok := isInvalidParam(err); ok {
		return api.ListGrants400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: bad}, nil
	}
	if err != nil {
		return nil, err
	}
	return api.ListGrants200JSONResponse{Items: items, Total: total, NextCursor: next, Source: s.source()}, nil
}

func (s *Server) GetGrant(ctx context.Context, req api.GetGrantRequestObject) (api.GetGrantResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := w.grant(req.Name)
	if !ok {
		return api.GetGrant404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse: notFound("MCPGrant", req.Name)}, nil
	}
	sum := grantSummary(g)
	out := api.Grant{
		Meta: sum.Meta, Spec: sum.Spec, Status: sum.Status,
		MissingCapabilities: []string{},
		SameSubject:         []string{},
		Source:              s.source(),
	}
	for _, name := range g.Spec.Capabilities {
		if _, ok := w.capability(name); !ok && !slices.Contains(out.MissingCapabilities, name) {
			out.MissingCapabilities = append(out.MissingCapabilities, name)
		}
	}
	for _, other := range w.grants {
		if other.Name != g.Name && other.Spec.Subject == g.Spec.Subject {
			out.SameSubject = append(out.SameSubject, other.Name)
		}
	}
	out.Instructions = s.Gateway.InstructionsOf(s.Gateway.Policy().ResolveCapabilities(g.Spec.Capabilities, s.catalogs()))
	return api.GetGrant200JSONResponse(out), nil
}

// ---------------------------------------------------------------- subjects

func (s *Server) ListSubjects(ctx context.Context, req api.ListSubjectsRequestObject) (api.ListSubjectsResponseObject, error) {
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	snap := s.Gateway.Policy()
	catalogs := s.catalogs()
	// Resolving a subject is the expensive part of this list, so it happens
	// for the subjects a page shows, and for every one only when the page is
	// sorted by what they are offered.
	rows := []subjectRow{}
	for _, subject := range snap.Subjects() {
		rows = append(rows, subjectRow{subject: subject, grants: grantsOf(w, subject), tools: sync.OnceValue(func() int {
			count := 0
			for _, ref := range snap.Resolve(subject, catalogs).Tools {
				if _, ok := s.offered(ref); ok {
					count++
				}
			}
			return count
		})})
	}
	p := req.Params
	rows, total, next, err := page(rows, subjectListing, newPageRequest(p.Limit, p.Cursor, p.Q, p.Sort))
	if bad, ok := isInvalidParam(err); ok {
		return api.ListSubjects400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse: bad}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]api.SubjectSummary, 0, len(rows))
	for _, r := range rows {
		items = append(items, api.SubjectSummary{Subject: r.subject, Grants: r.grants, ToolCount: r.tools()})
	}
	return api.ListSubjects200JSONResponse{Items: items, Total: total, NextCursor: next, Source: s.source()}, nil
}

func (s *Server) GetSubject(ctx context.Context, req api.GetSubjectRequestObject) (api.GetSubjectResponseObject, error) {
	subject := req.Params.Subject
	if subject == "" {
		return nil, errors.New("subject must not be empty")
	}
	w, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	catalogs := s.catalogs()
	res := s.Gateway.Policy().Resolve(subject, catalogs)
	paths := s.grantPaths(w, subject, catalogs)

	claimant := func(name string, ref policy.ToolRef) api.SubjectToolClaimant {
		c := api.SubjectToolClaimant{Ref: toolRef(ref), Paths: nonNil(paths[claimKey{name, ref}])}
		c.Definition, _ = s.offered(ref)
		return c
	}
	var tools []api.SubjectTool
	for _, name := range sortedKeys(res.Tools) {
		ref := res.Tools[name]
		t := api.SubjectTool{Name: name, State: api.SubjectToolStateUnavailable, Claimants: []api.SubjectToolClaimant{claimant(name, ref)}}
		if t.Claimants[0].Definition != nil {
			t.State = api.SubjectToolStateOffered
		}
		tools = append(tools, t)
	}
	for _, name := range res.ConflictNames() {
		t := api.SubjectTool{Name: name, State: api.SubjectToolStateConflict}
		for _, ref := range res.Conflicts[name] {
			t.Claimants = append(t.Claimants, claimant(name, ref))
		}
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	return api.GetSubject200JSONResponse(api.Subject{
		Subject:             subject,
		Grants:              grantsOf(w, subject),
		Tools:               nonNil(tools),
		MissingCapabilities: nonNil(res.MissingCapabilities),
		MissingToolSets:     nonNil(res.MissingToolSets),
		MissingServers:      nonNil(res.MissingServers),
		Instructions:        s.Gateway.Instructions(subject),
		Source:              s.source(),
	}), nil
}

// grantPaths maps every (name, backend tool) a subject's grants claim to the
// grant, capability and tool set each claim comes through.
func (s *Server) grantPaths(w *world, subject string, catalogs policy.Catalogs) map[claimKey][]api.GrantPath {
	out := map[claimKey][]api.GrantPath{}
	for _, g := range w.grants {
		if g.Spec.Subject != subject || !g.DeletionTimestamp.IsZero() {
			continue
		}
		for _, capName := range g.Spec.Capabilities {
			c, ok := w.capability(capName)
			if !ok {
				continue
			}
			for k, sets := range s.claimedThrough(w, c.Spec.ToolSets, catalogs) {
				for _, set := range sets {
					out[k] = append(out[k], api.GrantPath{Grant: g.Name, Capability: capName, ToolSet: set})
				}
			}
		}
	}
	return out
}

func grantsOf(w *world, subject string) []string {
	out := []string{}
	for _, g := range w.grants {
		if g.Spec.Subject == subject {
			out = append(out, g.Name)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nonNil keeps a required array from being written as null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
