package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
)

// discoveryTimeout bounds one catalog read: the handshake, if a session has to
// be opened, and every page of tools/list.
const discoveryTimeout = 15 * time.Second

// MCPServerReconciler discovers the catalog of each registered backend and
// reports it in status.
//
// The catalog itself stays in the Registry, in memory. The tool set reconciler
// verifies against it, which is why both run in the same process and why a
// freshly started controller briefly has nothing to verify against.
type MCPServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *discovery.Registry

	announcements chan event.TypedGenericEvent[*mcpv1alpha1.MCPServer]
}

// NewMCPServerReconciler builds an MCPServerReconciler. Wire the registry's
// OnToolsChanged to its Announce.
func NewMCPServerReconciler(c client.Client, scheme *runtime.Scheme, registry *discovery.Registry) *MCPServerReconciler {
	return &MCPServerReconciler{
		Client:        c,
		Scheme:        scheme,
		Registry:      registry,
		announcements: make(chan event.TypedGenericEvent[*mcpv1alpha1.MCPServer], 64),
	}
}

// Announce requests an immediate rediscovery, for a backend that sent
// notifications/tools/list_changed. It never blocks.
func (r *MCPServerReconciler) Announce(server string) {
	select {
	case r.announcements <- event.TypedGenericEvent[*mcpv1alpha1.MCPServer]{
		Object: &mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: server}},
	}:
	default:
	}
}

// +kubebuilder:rbac:groups=mcp-gateway.metapatrol.com,resources=mcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp-gateway.metapatrol.com,resources=mcpservers/status,verbs=get;update;patch

func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var server mcpv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			r.Registry.Forget(req.Name)
			observeResource("MCPServer", req.Name, nil)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !server.DeletionTimestamp.IsZero() {
		r.Registry.Forget(server.Name)
		return ctrl.Result{}, nil
	}

	r.Registry.Ensure(server.Name, server.Spec.Endpoint, server.Spec.Auth)
	discoverCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	catalog, changed, err := r.Registry.Discover(discoverCtx, server.Name)

	base := server.DeepCopy()
	server.Status.ObservedGeneration = server.Generation
	interval := server.Interval()
	var result ctrl.Result

	if err != nil {
		_, health, _ := r.Registry.Catalog(server.Name)
		logger.Info("discovery failed", "server", server.Name, "failures", health.Failures, "error", err.Error())
		// DiscoveredTools, CatalogRevision and LastDiscoveryTime are left as
		// they were: they describe the last discovery that worked.
		server.Status.Available = false
		setCondition(&server.Status.Conditions, server.Generation, mcpv1alpha1.ConditionReady, metav1.ConditionFalse,
			discoveryFailureReason(err), err.Error())
		result.RequeueAfter = min(discovery.RetryDelay(health.Failures), interval)
	} else {
		if changed {
			logger.Info("catalog changed", "server", server.Name, "revision", catalog.Revision, "tools", catalog.Len())
		}
		now := metav1.Now()
		server.Status.Available = true
		server.Status.DiscoveredTools = catalog.Len()
		server.Status.CatalogRevision = catalog.Revision
		server.Status.LastDiscoveryTime = &now
		setCondition(&server.Status.Conditions, server.Generation, mcpv1alpha1.ConditionReady, metav1.ConditionTrue,
			mcpv1alpha1.ReasonDiscovered, fmt.Sprintf("%d %s discovered", catalog.Len(), plural(catalog.Len(), "tool", "tools")))
		result.RequeueAfter = interval
	}

	if err := patchStatus(ctx, r.Client, &server, base); err != nil {
		return ctrl.Result{}, err
	}
	observeResource("MCPServer", server.Name, &server)
	return result, nil
}

func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Only spec changes. Every discovery writes status, and reacting to
		// that would turn the interval into a busy loop.
		For(&mcpv1alpha1.MCPServer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesRawSource(source.Channel(r.announcements, &handler.TypedEnqueueRequestForObject[*mcpv1alpha1.MCPServer]{})).
		WithOptions(controller.Options{
			// A backend that takes the full timeout to fail must not hold up
			// the others.
			MaxConcurrentReconciles: 4,
		}).
		Named("mcpserver").
		Complete(r)
}

// discoveryFailureReason tells credentials that could not be obtained, and
// credentials the backend refused, from a backend that is not there. The first
// two need someone to fix a Secret or a spec; the last may pass on its own.
func discoveryFailureReason(err error) string {
	var refused *discovery.RefusedError
	var credential *discovery.CredentialError
	switch {
	case errors.As(err, &credential):
		return mcpv1alpha1.ReasonCredentialsUnavailable
	case errors.As(err, &refused):
		return mcpv1alpha1.ReasonUnauthorized
	default:
		return mcpv1alpha1.ReasonDiscoveryFailed
	}
}
