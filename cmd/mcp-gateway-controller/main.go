package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/controller"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/credentials"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/metrics"
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
	_ = mcpv1alpha1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var logFormat string
	var connectTimeout time.Duration
	var secretNamespace string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for the controller.")
	flag.StringVar(&logFormat, "log-format", "json", "Log format: text or json")
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

	setupLog.Info("starting mcp-gateway-controller",
		"version", version,
		"commit", commit,
		"builtAt", builtAt,
	)

	metrics.SetBuildInfo("controller", version, commit, builtAt)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "mcp-gateway-controller.metapatrol.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	secrets := backendSecrets(mgr, secretNamespace)
	// The registry is wired to the server reconciler in both directions: the
	// reconciler fills it, and a backend announcing a changed tool list asks
	// the reconciler, through it, to look again.
	var servers *controller.MCPServerReconciler
	registry := discovery.NewRegistry(discovery.Options{
		Implementation: &mcp.Implementation{Name: "mcp-gateway-controller", Version: version},
		HTTPClient:     discovery.NewHTTPClient(),
		ConnectTimeout: connectTimeout,
		OnToolsChanged: func(server string) { servers.Announce(server) },
		Secrets:        secrets,
		Logger:         sdkLogger(ctrl.Log.WithName("mcp-client")),
	})
	defer registry.Close()
	servers = controller.NewMCPServerReconciler(mgr.GetClient(), mgr.GetScheme(), registry)

	controllers := []interface {
		SetupWithManager(ctrl.Manager) error
	}{
		servers,
		&controller.MCPToolSetReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Registry: registry},
		&controller.MCPCapabilityReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Registry: registry},
		&controller.MCPGrantReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Registry: registry},
	}
	for _, c := range controllers {
		if err := c.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// sdkLogger hands the MCP SDK, which logs through slog, the process's logr
// logger. The SDK logs protocol trouble at warn and error, which belongs in the
// same stream as everything else.
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
