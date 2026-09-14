// Package gateway is the data plane: one MCP endpoint in front of every
// registered backend, serving each caller exactly the tools its grants allow.
//
// # Shape
//
// The gateway keeps one MCP server per subject, not per session. All sessions
// of a subject see the same tools, so they share a server; the server's tool
// list is kept equal to what the subject's grants resolve to against the
// catalogs currently usable. When either side changes — a grant is edited, a
// backend publishes a new catalog, a backend has been down too long — the list
// is recomputed, and the SDK tells every connected session of that subject that
// its tools changed.
//
// # Enforcement
//
// The tool list is what an agent is shown, and it is not what authorises a
// call. Every tools/call is decided again, at the moment it arrives, against
// the current policy snapshot and the subject of the token that carried the
// request. A tool that was listed a minute ago and has since been revoked is
// refused, and a refusal looks exactly like a tool that does not exist.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/metrics"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/policy"
)

// Options configures a Gateway.
type Options struct {
	// Registry holds the backends and their catalogs.
	Registry *discovery.Registry

	// Implementation is how the gateway introduces itself to agents.
	Implementation *mcp.Implementation

	// Instructions, if set, are sent to agents when they start, ahead of the
	// instructions of the backends they reach.
	Instructions string

	// CatalogMaxAge is how long the tools of a backend whose discovery keeps
	// failing are still offered. Within it a blip goes unnoticed by agents;
	// past it the tools disappear from their lists, rather than staying
	// visible and failing on every call. Defaults to 5m.
	CatalogMaxAge time.Duration

	// CallTimeout bounds a forwarded tool call. Defaults to 5m.
	CallTimeout time.Duration

	// IdleServerTTL is how long the per-subject server of a subject with no
	// open session is kept. Defaults to 10m.
	IdleServerTTL time.Duration

	// Logger receives audit records and the gateway's own logging.
	Logger logr.Logger
}

func (o Options) withDefaults() Options {
	if o.Implementation == nil {
		o.Implementation = &mcp.Implementation{Name: "mcp-gate"}
	}
	if o.CatalogMaxAge <= 0 {
		o.CatalogMaxAge = 5 * time.Minute
	}
	if o.CallTimeout <= 0 {
		o.CallTimeout = 5 * time.Minute
	}
	if o.IdleServerTTL <= 0 {
		o.IdleServerTTL = 10 * time.Minute
	}
	return o
}

// Gateway serves the aggregated, filtered MCP endpoint.
type Gateway struct {
	opts Options
	log  logr.Logger

	policy       atomic.Pointer[policy.Snapshot]
	policyLoaded atomic.Bool

	mu       sync.Mutex
	subjects map[string]*subjectServer

	wake chan struct{}
}

// subjectServer is the MCP server of one subject and what is registered on it.
type subjectServer struct {
	server   *mcp.Server
	tools    map[string]exposed
	lastUsed time.Time
}

// exposed is one tool registered on a subject's server. tool is the catalog's
// own pointer, which is how a re-sync tells an unchanged tool from a changed
// one without comparing schemas.
type exposed struct {
	ref  policy.ToolRef
	tool *mcp.Tool
}

// New creates a Gateway. Until SetPolicy is called it grants nothing.
func New(opts Options) *Gateway {
	opts = opts.withDefaults()
	g := &Gateway{
		opts:     opts,
		log:      opts.Logger,
		subjects: map[string]*subjectServer{},
		wake:     make(chan struct{}, 1),
	}
	g.policy.Store(policy.Empty())
	return g
}

// SetPolicy replaces the policy snapshot and brings every subject's tool list
// in line with it.
func (g *Gateway) SetPolicy(s *policy.Snapshot) {
	g.policy.Store(s)
	g.policyLoaded.Store(true)
	g.Invalidate()
}

// Invalidate schedules a re-sync of every subject's tool list. Calls coalesce:
// ten invalidations while a sync is running produce one more sync, not ten.
func (g *Gateway) Invalidate() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

// Ready reports whether the gateway has read the policy at least once. Before
// that it would serve every agent an empty tool list, which is correct and
// useless, so it does not take traffic.
func (g *Gateway) Ready(*http.Request) error {
	if !g.policyLoaded.Load() {
		return errors.New("policy has not been loaded yet")
	}
	return nil
}

// Start runs the sync loop until ctx ends. It implements manager.Runnable.
func (g *Gateway) Start(ctx context.Context) error {
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-g.wake:
			g.syncAll()
		case <-sweep.C:
			// Also catches catalogs that aged out while nothing else
			// happened.
			g.syncAll()
		}
	}
}

// NeedLeaderElection is false: every replica serves traffic.
func (g *Gateway) NeedLeaderElection() bool { return false }

func (g *Gateway) syncAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for subject, ss := range g.subjects {
		if g.idle(ss, now) {
			delete(g.subjects, subject)
			continue
		}
		g.syncLocked(subject, ss)
	}
}

func (g *Gateway) idle(ss *subjectServer, now time.Time) bool {
	if now.Sub(ss.lastUsed) < g.opts.IdleServerTTL {
		return false
	}
	for range ss.server.Sessions() {
		return false
	}
	return true
}

// serverFor returns the MCP server of a subject, creating and populating it on
// first use. It is the getServer function of the Streamable HTTP handler.
func (g *Gateway) serverFor(subject string) *mcp.Server {
	g.mu.Lock()
	defer g.mu.Unlock()
	ss, ok := g.subjects[subject]
	if !ok {
		ss = &subjectServer{
			server: mcp.NewServer(g.opts.Implementation, &mcp.ServerOptions{
				// No Instructions here: they depend on what the subject may use
				// at the moment a session starts, and forwardInstructions writes
				// them into each answer instead.
				//
				// Tools, with change notifications — and nothing else. The
				// SDK advertises logging by default, which the gateway does
				// not relay.
				Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: true}},
				// Every list this server returns is filtered for one subject.
				// The protocol's default scope is "public", which lets an
				// intermediary serve one subject's tool list to another.
				SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
					c.CacheScope = "private"
				},
			}),
			tools: map[string]exposed{},
		}
		ss.server.AddReceivingMiddleware(g.auditRefusals(subject), g.forwardInstructions(subject))
		g.subjects[subject] = ss
		g.syncLocked(subject, ss)
	}
	ss.lastUsed = time.Now()
	return ss.server
}

// auditRefusals records the tools/call requests the SDK refuses by itself, for
// a tool that is not registered on the subject's server. callTool never sees
// those, and an attempt to call a tool outside one's grants is exactly what an
// audit record is for.
func (g *Gateway) auditRefusals(subject string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil && !g.registered(subject, call.Params.Name) {
				g.log.Info("tool call denied", "subject", subject, "tool", call.Params.Name, "reason", "no grant")
				metrics.RecordDenied(subject)
			}
			return next(ctx, method, req)
		}
	}
}

// forwardInstructions puts the backends' instructions into the answer a client
// gets when it starts: initialize, or server/discover under the stateless
// protocol.
//
// A backend's instructions tell a model how to use its tools — which one to
// reach for first, what an argument means, what a partial answer looks like.
// An agent behind the gateway would otherwise see the tools without any of
// that. They are assembled per request, because they follow the grants: a
// subject sees the instructions of the backends it holds at least one tool
// from, and nothing of the others.
func (g *Gateway) forwardInstructions(subject string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			switch r := res.(type) {
			case *mcp.InitializeResult:
				r.Instructions = g.instructionsFor(subject)
			case *mcp.DiscoverResult:
				r.Instructions = g.instructionsFor(subject)
			}
			return res, nil
		}
	}
}

// instructionsFor assembles what a subject is told: the gateway's own
// instructions, if any, then one section per backend it reaches, in server name
// order.
//
// Each section names the backend and the tools the subject has from it. Tools
// are exposed under their bare names, so without that line a model reading
// "use search before get" cannot tell which of its tools the sentence is about.
// A backend's text is passed on as it is, including what it says about tools
// the subject was not granted; it is prose, and there is nothing to filter it
// by.
func (g *Gateway) instructionsFor(subject string) string {
	byServer := map[string][]string{}
	for name, w := range g.desired(subject) {
		byServer[w.ref.Server] = append(byServer[w.ref.Server], name)
	}
	servers := make([]string, 0, len(byServer))
	for server := range byServer {
		servers = append(servers, server)
	}
	sort.Strings(servers)

	var b strings.Builder
	b.WriteString(strings.TrimSpace(g.opts.Instructions))
	for _, server := range servers {
		catalog, ok := g.usableCatalog(server)
		if !ok {
			continue
		}
		text := strings.TrimSpace(catalog.Instructions)
		if text == "" {
			continue
		}
		tools := byServer[server]
		sort.Strings(tools)
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## %s\n\nTools from this server: %s\n\n%s", server, strings.Join(tools, ", "), text)
	}
	return b.String()
}

func (g *Gateway) registered(subject, name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	ss, ok := g.subjects[subject]
	if !ok {
		return false
	}
	_, ok = ss.tools[name]
	return ok
}

// desired computes what a subject should currently see.
func (g *Gateway) desired(subject string) map[string]exposed {
	res := g.policy.Load().Resolve(subject)
	out := make(map[string]exposed, len(res.Tools))
	for name, ref := range res.Tools {
		catalog, ok := g.usableCatalog(ref.Server)
		if !ok {
			continue
		}
		tool, ok := catalog.Tool(ref.Tool)
		if !ok {
			continue
		}
		out[name] = exposed{ref: ref, tool: tool}
	}
	return out
}

// usableCatalog returns a backend's catalog while it may still be served.
func (g *Gateway) usableCatalog(server string) (*discovery.Catalog, bool) {
	catalog, health, ok := g.opts.Registry.Catalog(server)
	if !ok {
		return nil, false
	}
	if health.Failures > 0 && time.Since(health.LastSuccess) > g.opts.CatalogMaxAge {
		return nil, false
	}
	return catalog, true
}

// syncLocked brings one subject's registered tools in line with desired. Only
// actual differences touch the server, because every AddTool and RemoveTools
// sends connected sessions a list_changed notification.
func (g *Gateway) syncLocked(subject string, ss *subjectServer) {
	want := g.desired(subject)

	var remove []string
	for name := range ss.tools {
		if _, ok := want[name]; !ok {
			remove = append(remove, name)
		}
	}

	for name, w := range want {
		if have, ok := ss.tools[name]; ok && have.tool == w.tool && have.ref == w.ref {
			continue
		}
		// AddTool replaces a tool of the same name, so a changed definition
		// needs no removal first.
		if err := g.register(subject, ss.server, name, w); err != nil {
			g.log.Error(err, "backend tool cannot be exposed", "tool", w.ref.String())
			if _, had := ss.tools[name]; had {
				remove = append(remove, name)
			}
			continue
		}
		ss.tools[name] = w
	}

	if len(remove) > 0 {
		ss.server.RemoveTools(remove...)
		for _, name := range remove {
			delete(ss.tools, name)
		}
	}
}

// register adds one tool to a subject's server.
//
// The definition is the backend's, under the exposed name. The SDK panics on a
// definition it considers malformed — an input schema that is not an object,
// say — and a backend's catalog is not something the gateway gets to vouch
// for, so the panic is turned into an error and the one tool is left out.
func (g *Gateway) register(subject string, server *mcp.Server, name string, w exposed) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the SDK refused the definition: %v", r)
		}
	}()
	tool := *w.tool
	tool.Name = name
	server.AddTool(&tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return g.callTool(ctx, subject, req)
	})
	return nil
}

// errUnknownTool is what a refused call receives. It is word for word what the
// SDK answers for a tool that is not registered at all, so a caller cannot
// probe for tools it holds no grant on.
func errUnknownTool(name string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", name)}
}

// callTool authorises and forwards one tool call.
func (g *Gateway) callTool(ctx context.Context, serverSubject string, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.Params.Name

	// The subject is taken from the token on this very request, not from the
	// server it arrived at. The SDK already refuses to continue a session
	// under a different token subject; this does not rely on it.
	var info *sdkauth.TokenInfo
	if req.Extra != nil {
		info = req.Extra.TokenInfo
	}
	if info == nil || info.UserID == "" || info.UserID != serverSubject {
		g.log.Info("tool call denied", "subject", serverSubject, "tool", name, "reason", "subject mismatch")
		metrics.RecordDenied(serverSubject)
		return nil, errUnknownTool(name)
	}
	subject := info.UserID
	audit := g.log.WithValues("subject", subject, "tool", name)
	if clientID, ok := info.Extra["client_id"].(string); ok {
		audit = audit.WithValues("clientID", clientID)
	}

	ref, ok := g.policy.Load().Authorize(subject, name)
	if !ok {
		audit.Info("tool call denied", "reason", "no grant")
		metrics.RecordDenied(subject)
		return nil, errUnknownTool(name)
	}
	audit = audit.WithValues("server", ref.Server)

	catalog, ok := g.usableCatalog(ref.Server)
	backend, registered := g.opts.Registry.Backend(ref.Server)
	if !ok || !registered {
		audit.Info("tool call failed", "reason", "backend unavailable")
		metrics.RecordToolCall(ref.Server, ref.Tool, metrics.CallBackendError, 0)
		return unavailableResult(), nil
	}
	if _, exposedNow := catalog.Tool(ref.Tool); !exposedNow {
		audit.Info("tool call denied", "reason", "backend does not expose the tool")
		return nil, errUnknownTool(name)
	}

	callCtx, cancel := context.WithTimeout(ctx, g.opts.CallTimeout)
	defer cancel()
	start := time.Now()
	res, err := backend.CallTool(callCtx, &mcp.CallToolParams{
		Name:      ref.Tool,
		Arguments: req.Params.Arguments,
	})
	elapsed := time.Since(start)

	switch {
	case err != nil:
		metrics.RecordToolCall(ref.Server, ref.Tool, metrics.CallBackendError, elapsed.Seconds())
		// A JSON-RPC error from the backend — invalid arguments, typically —
		// is the backend's answer and goes back as it came.
		if wire, ok := discovery.BackendError(err); ok {
			audit.Info("tool call rejected by backend", "code", wire.Code, "duration", elapsed)
			return nil, wire
		}
		audit.Error(err, "tool call failed", "duration", elapsed)
		return unavailableResult(), nil
	case res.IsError:
		metrics.RecordToolCall(ref.Server, ref.Tool, metrics.CallToolError, elapsed.Seconds())
		audit.Info("tool call returned an error", "duration", elapsed)
	default:
		metrics.RecordToolCall(ref.Server, ref.Tool, metrics.CallOK, elapsed.Seconds())
		audit.Info("tool call", "duration", elapsed)
	}
	return res, nil
}

// unavailableResult reports a backend failure as a tool error, which the model
// on the other end sees and can act on. The cause is logged, not returned: it
// carries backend addresses the caller has no business learning.
func unavailableResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: "mcp-gate: the backend serving this tool is currently unavailable"}},
	}
}
