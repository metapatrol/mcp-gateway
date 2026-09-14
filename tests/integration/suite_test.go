// Package integration runs the controller and the gateway against a real API
// server with the generated CRDs installed, and real MCP backends in-process.
//
// It needs the envtest binaries — `make integration-test` fetches them and sets
// KUBEBUILDER_ASSETS. Without that variable the tests skip.
package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/controller"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/gateway"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	scheme    = runtime.NewScheme()
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		// Nothing to run against; `go test ./...` stays green without envtest.
		os.Exit(0)
	}
	_ = clientgoscheme.AddToScheme(scheme)
	_ = gatev1alpha1.AddToScheme(scheme)
	ctrl.SetLogger(logr.Discard())

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "charts", "mcp-gate", "crds")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	cfg, err = env.Start()
	if err != nil {
		panic(err)
	}
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}

	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

// startController runs mcp-gate-controller's reconcilers, as its main does.
func startController(t *testing.T) {
	t.Helper()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		// Each test starts its own managers; controller names must not
		// collide with the previous test's.
		Controller: controllerConfig(),
	})
	require.NoError(t, err)

	var servers *controller.MCPServerReconciler
	registry := discovery.NewRegistry(discovery.Options{
		ConnectTimeout: 2 * time.Second,
		OnToolsChanged: func(server string) { servers.Announce(server) },
	})
	t.Cleanup(registry.Close)
	servers = controller.NewMCPServerReconciler(mgr.GetClient(), scheme, registry)

	for _, r := range []interface{ SetupWithManager(ctrl.Manager) error }{
		servers,
		&controller.MCPToolSetReconciler{Client: mgr.GetClient(), Scheme: scheme, Registry: registry},
		&controller.MCPCapabilityReconciler{Client: mgr.GetClient(), Scheme: scheme, Registry: registry},
		&controller.MCPGrantReconciler{Client: mgr.GetClient(), Scheme: scheme, Registry: registry},
	} {
		require.NoError(t, r.SetupWithManager(mgr))
	}
	run(t, mgr)
}

// startGateway runs mcp-gate-gateway, as its main does, and returns the URL of
// its MCP endpoint.
func startGateway(t *testing.T) (string, *gateway.Gateway) {
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
	})
	t.Cleanup(registry.Close)
	gw := gateway.New(gateway.Options{Registry: registry, Logger: logr.Discard()})
	catalogs = gateway.NewCatalogReconciler(mgr.GetClient(), registry, gw)
	require.NoError(t, catalogs.SetupWithManager(mgr))
	require.NoError(t, (&gateway.PolicyReconciler{Client: mgr.GetClient(), Gateway: gw}).SetupWithManager(mgr))
	require.NoError(t, mgr.Add(gw))

	handler, err := gw.Handler(gateway.HandlerOptions{Verifier: verifier})
	require.NoError(t, err)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	run(t, mgr)
	return srv.URL + "/mcp", gw
}

func run(t *testing.T, mgr ctrl.Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = mgr.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func controllerConfig() config.Controller {
	skip := true
	return config.Controller{SkipNameValidation: &skip}
}

// verifier treats a token as the subject's name. Token verification is tested
// in internal/auth; here it is only the way a subject gets in.
func verifier(_ context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
	if token == "" {
		return nil, sdkauth.ErrInvalidToken
	}
	return &sdkauth.TokenInfo{UserID: token, Expiration: time.Now().Add(time.Hour)}, nil
}

func connect(t *testing.T, url, subject string, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, opts).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer(subject)}}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}
