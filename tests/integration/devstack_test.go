package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/gateway"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/mcptest"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/ui"
)

// TestDevStack is not a test. It runs everything the console needs on this
// machine — an API server with the CRDs, MCP backends, the controller, the
// gateway with its inspect API, and mcp-gateway-ui — with example resources
// that show every state the console has a way of showing, and keeps it
// running until interrupted. `make dev-stack` starts it.
//
// The console is at DEVSTACK_ADDR (default 127.0.0.1:8480), which is where
// `npm start` in web/ui proxies /api to as well. Agents can use the gateway's
// MCP endpoint it prints, with the subject as the bearer token.
func TestDevStack(t *testing.T) {
	if os.Getenv("DEVSTACK") == "" {
		t.Skip("set DEVSTACK=1, or run make dev-stack")
	}
	ctx := context.Background()

	backends := devBackends(t)
	for _, obj := range devResources(backends) {
		require.NoError(t, k8sClient.Create(ctx, obj))
	}
	startController(t)
	mcpURL, inspectURL, _ := startGatewayWithInspect(t)
	kubeconfig := writeKubeconfig(t)

	addr := os.Getenv("DEVSTACK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8480"
	}
	gatewayURL, err := url.Parse(inspectURL)
	require.NoError(t, err)
	built := ui.Built()
	console := &http.Server{
		Addr: addr,
		Handler: ui.Handler(ui.Options{
			UI:      built,
			Gateway: gatewayURL,
			Token:   func() (string, error) { return "dev-stack", nil },
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	require.NoError(t, err, "is something else listening on %s? set DEVSTACK_ADDR", addr)
	go func() { _ = console.Serve(ln) }()
	t.Cleanup(func() { _ = console.Close() })

	fmt.Printf("\n  console      http://%s/\n", addr)
	if built == nil {
		fmt.Println("               (built without the web UI: run make ui-build, or npm start in web/ui)")
	}
	fmt.Printf("  inspect API  %s/api/v1alpha1/\n", inspectURL)
	fmt.Printf("  MCP          %s   (bearer token = subject, e.g. agent-coder)\n", mcpURL)
	fmt.Printf("  kubectl      kubectl --kubeconfig %s get mcp-gateway\n", kubeconfig)
	fmt.Println("               (edit a resource there and watch the console follow)")
	fmt.Println()
	fmt.Println("  Ctrl-C stops it.")

	stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
}

// startGatewayWithInspect runs the gateway as its main does with the inspect
// API and its event stream on, and returns the URLs of both. The inspect API
// takes every caller: there is no API server token to review here, and the
// listener is local.
func startGatewayWithInspect(t *testing.T) (mcpURL, inspectURL string, events *inspect.Events) {
	t.Helper()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:     scheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: controllerConfig(),
	})
	require.NoError(t, err)

	var catalogs *gateway.CatalogReconciler
	registry := discovery.NewRegistry(discovery.Options{
		ConnectTimeout: 2 * time.Second,
		OnToolsChanged: func(server string) { catalogs.Announce(server) },
		Secrets:        credentials.NewSecretReader(mgr.GetAPIReader(), secretNamespace),
	})
	t.Cleanup(registry.Close)
	gw := gateway.New(gateway.Options{
		Registry:      registry,
		Logger:        logr.Discard(),
		CatalogMaxAge: 30 * time.Second,
		Instructions:  "You are connected to mcp-gateway on a dev stack.",
	})
	catalogs = gateway.NewCatalogReconciler(mgr.GetClient(), registry, gw)
	events = &inspect.Events{Cache: mgr.GetCache()}
	catalogs.OnDiscovery = events.PublishServer
	require.NoError(t, catalogs.SetupWithManager(mgr))
	require.NoError(t, (&gateway.PolicyReconciler{Client: mgr.GetClient(), Gateway: gw}).SetupWithManager(mgr))
	require.NoError(t, mgr.Add(gw))
	require.NoError(t, mgr.Add(events))

	handler, err := gw.Handler(gateway.HandlerOptions{Verifier: verifier})
	require.NoError(t, err)
	mcpSrv := httptest.NewServer(handler)
	t.Cleanup(mcpSrv.Close)

	pass := func(h http.Handler) http.Handler { return h }
	inspectSrv := httptest.NewServer(inspect.Handler(&inspect.Server{
		Reader:   mgr.GetClient(),
		Gateway:  gw,
		Registry: registry,
		Replica:  "dev-stack",
		Events:   events,
	}, pass, logr.Discard()))
	t.Cleanup(inspectSrv.Close)

	run(t, mgr)
	return mcpSrv.URL + "/mcp", inspectSrv.URL, events
}

// writeKubeconfig writes a kubeconfig for the envtest API server, so the
// resources can be changed with kubectl while the dev stack runs.
func writeKubeconfig(t *testing.T) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters["dev-stack"] = &clientcmdapi.Cluster{Server: cfg.Host, CertificateAuthorityData: cfg.CAData}
	config.AuthInfos["dev-stack"] = &clientcmdapi.AuthInfo{ClientCertificateData: cfg.CertData, ClientKeyData: cfg.KeyData, Token: cfg.BearerToken}
	config.Contexts["dev-stack"] = &clientcmdapi.Context{Cluster: "dev-stack", AuthInfo: "dev-stack"}
	config.CurrentContext = "dev-stack"
	file := filepath.Join(os.TempDir(), "mcp-gateway-dev-stack.kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(*config, file))
	t.Cleanup(func() { _ = os.Remove(file) })
	return file
}

// tool builds a definition with a real-looking schema: properties as name →
// [type, description], required ones named.
func tool(name, description string, readOnly bool, props map[string][2]string, required ...string) *mcp.Tool {
	properties := map[string]any{}
	for p, v := range props {
		properties[p] = map[string]any{"type": v[0], "description": v[1]}
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required})
	t := &mcp.Tool{Name: name, Description: description, InputSchema: json.RawMessage(schema)}
	if readOnly {
		t.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
	} else {
		t.Annotations = &mcp.ToolAnnotations{DestructiveHint: ptrTo(true)}
	}
	return t
}

func ptrTo[T any](v T) *T { return &v }

func devBackends(t *testing.T) map[string]*mcptest.Backend {
	t.Helper()
	b := map[string]*mcptest.Backend{
		"source-intelligence": mcptest.NewBackendWithInstructions(t, "source-intelligence",
			"Search and read the source of every repository this platform indexes. Every response carries a \"completeness\" field; treat a partial answer as incomplete evidence rather than as proof that something does not exist."),
		"artifact": mcptest.NewBackendWithInstructions(t, "artifact",
			"Before starting work, search whether somebody already published the answer. Publish results that are worth keeping."),
		"memory":  mcptest.NewBackend(t, "memory"),
		"tickets": mcptest.NewBackend(t, "tickets"),
		"search":  mcptest.NewBackend(t, "search"),
		"weather": mcptest.NewBackendWithInstructions(t, "weather", "Resolve a place with search first; the other tools take its location_id."),
	}
	repo := [2]string{"string", "repository name, <git-server>/<path>"}
	for _, d := range []*mcp.Tool{
		tool("code.search", "Search the source of every indexed repository. Mode \"symbols\" (default) matches declaration names approximately; mode \"text\" matches the literal bytes of the files and is what you want for strings, env vars and comments.", true,
			map[string][2]string{"query": {"string", "term to look for"}, "mode": {"string", "symbols or text"}, "repository": repo, "limit": {"integer", "default 50"}}, "query"),
		tool("code.symbol", "Look up one declaration by its exact name and return its signature, doc comment and location.", true,
			map[string][2]string{"name": {"string", ""}, "repository": repo}, "name"),
		tool("code.references", "List the places that refer to a declaration. Use after code.symbol to see who calls a function.", true,
			map[string][2]string{"symbol_id": {"string", "from code.symbol"}}, "symbol_id"),
		tool("repositories.list", "List the repositories this platform indexes.", true, map[string][2]string{"filter": {"string", "substring of the name"}}),
		tool("repositories.get", "Describe one repository: default branch, indexed commit, index state.", true, map[string][2]string{"repository": repo}, "repository"),
		tool("repositories.list_files", "List the files of a repository at a commit.", true,
			map[string][2]string{"repository": repo, "ref": {"string", "default branch if omitted"}, "path": {"string", "directory to list"}}, "repository"),
		tool("repositories.read_file", "Read one file of a repository at a commit.", true,
			map[string][2]string{"repository": repo, "path": {"string", ""}, "ref": {"string", ""}}, "repository", "path"),
		tool("refs.list", "List the branches and tags of a repository.", true, map[string][2]string{"repository": repo}, "repository"),
	} {
		b["source-intelligence"].AddDefinedTool(d)
	}
	id := map[string][2]string{"id": {"string", "the artifact's URL or ID"}}
	for _, d := range []*mcp.Tool{
		tool("artifact_search", "Find published artifacts by title, description or content. Use before starting work to check whether somebody already published the answer.", true,
			map[string][2]string{"query": {"string", ""}, "limit": {"integer", ""}}, "query"),
		tool("artifact_get", "Fetch one artifact's content and metadata.", true, id, "id"),
		tool("artifact_publish", "Publish a new artifact and return its permanent URL. Use when a result is worth keeping: a report, a table, a chart.", false,
			map[string][2]string{"title": {"string", ""}, "content": {"string", "HTML or Markdown"}, "description": {"string", ""}}, "title", "content"),
		tool("artifact_update", "Publish a new revision of an existing artifact, keeping its URL.", false, map[string][2]string{"id": id["id"], "content": {"string", ""}}, "id", "content"),
		tool("artifact_delete", "Delete an artifact permanently. Its URL stops working for everyone.", false, id, "id"),
		tool("artifact_revisions", "List the revisions of an artifact, newest first.", true, id, "id"),
	} {
		b["artifact"].AddDefinedTool(d)
	}
	for _, d := range []*mcp.Tool{
		tool("memory_search", "Recall what was stored earlier. Search before asking the user something they may already have told you.", true,
			map[string][2]string{"query": {"string", ""}, "limit": {"integer", ""}}, "query"),
		tool("memory_store", "Remember a fact, a preference or a decision for later sessions.", false, map[string][2]string{"text": {"string", ""}, "tags": {"array", ""}}, "text"),
		tool("memory_list", "List stored memories, newest first.", true, map[string][2]string{"limit": {"integer", ""}}),
		tool("memory_forget", "Delete a stored memory.", false, map[string][2]string{"id": {"string", ""}}, "id"),
	} {
		b["memory"].AddDefinedTool(d)
	}
	for _, d := range []*mcp.Tool{
		tool("tickets_list", "List support tickets, filtered by state or assignee.", true, map[string][2]string{"state": {"string", "open or closed"}}),
		tool("tickets_get", "Read one ticket with its full conversation.", true, map[string][2]string{"id": {"string", ""}}, "id"),
	} {
		b["tickets"].AddDefinedTool(d)
	}
	for _, d := range []*mcp.Tool{
		tool("search", "Search the web and return titles, URLs and snippets.", true, map[string][2]string{"q": {"string", ""}, "count": {"integer", "default 10"}}, "q"),
		tool("fetch", "Fetch a web page and return its main text.", true, map[string][2]string{"url": {"string", ""}}, "url"),
	} {
		b["search"].AddDefinedTool(d)
	}
	for _, d := range []*mcp.Tool{
		tool("get_forecast", "Weather forecast for a location, hourly for 48 hours and daily for 7 days.", true,
			map[string][2]string{"location_id": {"string", "from search"}, "units": {"string", "metric or imperial"}}, "location_id"),
		tool("get_alerts", "Active severe weather warnings for a location.", true, map[string][2]string{"location_id": {"string", ""}}, "location_id"),
		tool("search", "Find a location by name or postcode and return its location_id.", true, map[string][2]string{"query": {"string", ""}}, "query"),
	} {
		b["weather"].AddDefinedTool(d)
	}
	// tickets refuses everyone: its discovery fails with Unauthorized.
	b["tickets"].Guard(func(*http.Request) int { return http.StatusUnauthorized })
	return b
}

func devResources(b map[string]*mcptest.Backend) []client.Object {
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name} }
	server := func(name string, auth *mcpv1alpha1.BackendAuth) *mcpv1alpha1.MCPServer {
		return &mcpv1alpha1.MCPServer{ObjectMeta: meta(name), Spec: mcpv1alpha1.MCPServerSpec{
			Endpoint: b[name].URL, DiscoveryInterval: &metav1.Duration{Duration: 15 * time.Second}, Auth: auth,
		}}
	}
	toolSet := func(name string, spec mcpv1alpha1.MCPToolSetSpec) *mcpv1alpha1.MCPToolSet {
		return &mcpv1alpha1.MCPToolSet{ObjectMeta: meta(name), Spec: spec}
	}
	capability := func(name, description string, sets ...string) *mcpv1alpha1.MCPCapability {
		return &mcpv1alpha1.MCPCapability{ObjectMeta: meta(name), Spec: mcpv1alpha1.MCPCapabilitySpec{Description: description, ToolSets: sets}}
	}
	grant := func(name, subject string, caps ...string) *mcpv1alpha1.MCPGrant {
		return &mcpv1alpha1.MCPGrant{ObjectMeta: meta(name), Spec: mcpv1alpha1.MCPGrantSpec{Subject: subject, Capabilities: caps}}
	}
	return []client.Object{
		server("source-intelligence", nil),
		server("artifact", nil),
		server("memory", &mcpv1alpha1.BackendAuth{Type: mcpv1alpha1.BackendAuthPassthrough}),
		server("tickets", &mcpv1alpha1.BackendAuth{Type: mcpv1alpha1.BackendAuthPassthrough, Passthrough: &mcpv1alpha1.PassthroughAuth{
			Discovery: &mcpv1alpha1.DiscoveryAuth{APIKey: &mcpv1alpha1.APIKeyAuth{SecretRef: mcpv1alpha1.SecretKeyRef{Name: "tickets-discovery", Key: "token"}}},
		}}),
		server("search", nil),
		server("weather", nil),

		toolSet("source-intelligence-analysis", mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "source-intelligence", Tools: []string{"code.*", "repositories.*", "refs.list"}, Exclude: []string{"repositories.read_file"},
		}),
		toolSet("artifact-read", mcpv1alpha1.MCPToolSetSpec{ServerRef: "artifact", Tools: []string{"artifact_search", "artifact_get"}}),
		toolSet("artifact-readwrite", mcpv1alpha1.MCPToolSetSpec{ServerRef: "artifact", Tools: []string{"artifact_*"}, Exclude: []string{"artifact_delete"}}),
		toolSet("memory-engram", mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "memory", Tools: []string{"memory_*"}, Exclude: []string{"memory_forget"}, Prefix: "engram_",
			Aliases: map[string]mcpv1alpha1.ToolName{"memory_search": "recall"},
		}),
		toolSet("ticket-read", mcpv1alpha1.MCPToolSetSpec{ServerRef: "tickets", Tools: []string{"tickets_list", "tickets_get"}}),
		toolSet("web-search", mcpv1alpha1.MCPToolSetSpec{ServerRef: "search", Tools: []string{"search", "fetch_page"}}),
		toolSet("weather-basic", mcpv1alpha1.MCPToolSetSpec{ServerRef: "weather", Tools: []string{"*"}}),

		capability("code-research", "Read and search source code, read published artifacts, and remember findings.",
			"source-intelligence-analysis", "artifact-read", "memory-engram"),
		capability("artifact-authoring", "Search, read and publish artifacts.", "artifact-readwrite"),
		capability("field-ops", "Weather and web search for field teams.", "web-search", "weather-basic"),
		capability("ticket-desk", "Read support tickets.", "ticket-read"),

		grant("coder-agent", "agent-coder", "code-research", "artifact-authoring"),
		grant("coder-web", "agent-coder", "web-research"),
		grant("support-bot", "https://idp.example.com/clients/svc-support", "field-ops", "ticket-desk"),
	}
}
