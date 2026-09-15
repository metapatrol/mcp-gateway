package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/auth"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/gateway"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/metrics"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"

	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = gatev1alpha1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var probeAddr string
	var mcpAddr string
	var mcpPath string
	var logFormat string

	var authCfg auth.Config
	var introspectionSecretFile string
	var resourceURL string
	var authorizationServers string
	var scopesSupported string

	var catalogMaxAge time.Duration
	var callTimeout time.Duration
	var connectTimeout time.Duration
	var secretNamespace string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.StringVar(&mcpAddr, "mcp-bind-address", ":8090", "The address the MCP endpoint binds to.")
	flag.StringVar(&mcpPath, "mcp-path", "/mcp", "The path the MCP endpoint is served at.")
	flag.StringVar(&logFormat, "log-format", "json", "Log format: text or json")

	flag.StringVar(&authCfg.Issuer, "oauth-issuer", "", "Expected iss claim. Empty skips the check.")
	flag.StringVar(&authCfg.Audience, "oauth-audience", "", "Expected aud claim. Empty skips the check.")
	flag.StringVar(&authCfg.JWKSURL, "oauth-jwks-url", "", "JWKS URL to verify JWT access tokens against.")
	flag.StringVar(&authCfg.IntrospectionURL, "oauth-introspection-url", "", "RFC 7662 introspection endpoint for opaque access tokens.")
	flag.StringVar(&authCfg.IntrospectionClientID, "oauth-introspection-client-id", "", "Client ID the gateway authenticates to the introspection endpoint with.")
	flag.StringVar(&introspectionSecretFile, "oauth-introspection-client-secret-file", "", "File holding the introspection client secret. MCP_GATE_INTROSPECTION_CLIENT_SECRET is read when unset.")
	flag.DurationVar(&authCfg.IntrospectionCacheTTL, "oauth-introspection-cache-ttl", 30*time.Second, "How long an introspection answer is reused; also the longest a revoked token keeps working.")
	flag.StringVar(&authCfg.SubjectClaim, "oauth-subject-claim", "sub", "Claim the subject is read from. A dotted path reads a nested claim.")
	flag.StringVar(&resourceURL, "resource-url", "", "The gateway's public MCP URL, published as RFC 9728 protected resource metadata.")
	flag.StringVar(&authorizationServers, "authorization-servers", "", "Comma-separated issuer URLs published in the protected resource metadata.")
	flag.StringVar(&scopesSupported, "scopes-supported", "", "Comma-separated scopes published in the protected resource metadata.")

	flag.DurationVar(&catalogMaxAge, "catalog-max-age", 5*time.Minute, "How long the tools of a backend whose discovery keeps failing are still offered.")
	flag.DurationVar(&callTimeout, "call-timeout", 5*time.Minute, "Upper bound on a forwarded tool call.")
	flag.DurationVar(&connectTimeout, "backend-connect-timeout", 10*time.Second, "How long the MCP handshake with a backend may take.")
	flag.StringVar(&secretNamespace, "secret-namespace", "", "Namespace the Secrets named in MCPServer spec.auth are read from. Defaults to the namespace this pod runs in.")
	flag.Parse()

	if logFormat != "text" && logFormat != "json" {
		fmt.Fprintf(os.Stderr, "invalid --log-format %q: must be text or json\n", logFormat)
		os.Exit(1)
	}

	opts := zap.Options{
		Development: logFormat == "text",
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("starting mcp-gate-gateway",
		"version", version,
		"commit", commit,
		"builtAt", builtAt,
	)

	metrics.SetBuildInfo("gateway", version, commit, builtAt)

	secret, err := readSecret(introspectionSecretFile, "MCP_GATE_INTROSPECTION_CLIENT_SECRET")
	if err != nil {
		setupLog.Error(err, "unable to read the introspection client secret")
		os.Exit(1)
	}
	authCfg.IntrospectionClientSecret = secret
	verifier, err := auth.NewVerifier(authCfg)
	if err != nil {
		setupLog.Error(err, "invalid token verification settings")
		os.Exit(1)
	}
	setupLog.Info("verifying tokens", "config", verifier.String())

	// Every replica is active: no leader election. The gateway reads the API
	// and never writes it.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	secrets := backendSecrets(mgr, secretNamespace)
	gatewayLog := ctrl.Log.WithName("gateway")
	var catalogs *gateway.CatalogReconciler
	registry := discovery.NewRegistry(discovery.Options{
		Implementation: &mcp.Implementation{Name: "mcp-gate-gateway", Version: version},
		HTTPClient:     discovery.NewHTTPClient(),
		ConnectTimeout: connectTimeout,
		OnToolsChanged: func(server string) { catalogs.Announce(server) },
		Secrets:        secrets,
		Logger:         sdkLogger(ctrl.Log.WithName("mcp-client")),
	})
	defer registry.Close()

	gw := gateway.New(gateway.Options{
		Registry:       registry,
		Implementation: &mcp.Implementation{Name: "mcp-gate", Version: version},
		CatalogMaxAge:  catalogMaxAge,
		CallTimeout:    callTimeout,
		Logger:         gatewayLog,
	})
	catalogs = gateway.NewCatalogReconciler(mgr.GetClient(), registry, gw)

	controllers := []interface {
		SetupWithManager(ctrl.Manager) error
	}{
		catalogs,
		&gateway.PolicyReconciler{Client: mgr.GetClient(), Gateway: gw},
	}
	for _, c := range controllers {
		if err := c.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller")
			os.Exit(1)
		}
	}

	handler, err := gw.Handler(gateway.HandlerOptions{
		Path:                 mcpPath,
		Verifier:             verifier.Verify,
		ResourceURL:          resourceURL,
		AuthorizationServers: splitList(authorizationServers),
		ScopesSupported:      splitList(scopesSupported),
		Logger:               sdkLogger(ctrl.Log.WithName("mcp-server")),
	})
	if err != nil {
		setupLog.Error(err, "unable to build the MCP handler")
		os.Exit(1)
	}
	if err := mgr.Add(gw); err != nil {
		setupLog.Error(err, "unable to add the tool list sync")
		os.Exit(1)
	}
	if err := mgr.Add(&gateway.HTTPServer{Addr: mcpAddr, Handler: handler, Log: gatewayLog.Info}); err != nil {
		setupLog.Error(err, "unable to add the MCP server")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("policy", gw.Ready); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// readSecret reads a secret from a file, or from an environment variable when
// no file is named. Neither is a flag value, which would put the secret in the
// process list.
func readSecret(file, env string) (string, error) {
	if file == "" {
		return os.Getenv(env), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func sdkLogger(log logr.Logger) *slog.Logger {
	return slog.New(logr.ToSlogHandler(log))
}

// backendSecrets reads the Secrets MCPServers name for their credentials. It
// uses the API reader, not the manager's cache: a cached client would watch
// every Secret in the namespace to serve the few that are named.
func backendSecrets(mgr ctrl.Manager, namespace string) credentials.Secrets {
	if namespace == "" {
		namespace = credentials.PodNamespace()
	}
	if namespace == "" {
		setupLog.Info("no namespace to read backend credentials from: MCPServers whose spec.auth names a Secret will fail discovery; set --secret-namespace")
		return nil
	}
	setupLog.Info("reading backend credentials", "namespace", namespace)
	return credentials.NewSecretReader(mgr.GetAPIReader(), namespace)
}
