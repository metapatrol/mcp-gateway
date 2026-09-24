package gateway

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// The gateway watches the API and never writes to it. Both reconcilers below run
// on every replica — neither needs leader election — and neither touches
// status: reporting is the controller's job, and a gateway that wrote status
// would have as many opinions about a resource as it has replicas.

// CatalogReconciler keeps the gateway's own copy of each backend's catalog.
//
// It does not wait for the controller. The controller discovering a new catalog
// shows up here as a changed status.catalogRevision and triggers a rediscovery,
// which makes changes arrive sooner; but the gateway also rediscovers on its own
// interval, so a controller that is down delays nothing.
type CatalogReconciler struct {
	client.Client
	Registry *discovery.Registry
	Gateway  *Gateway

	// OnDiscovery, if set, is told when a discovery changed what this replica
	// knows about a backend: a new catalog, or discovery starting or
	// stopping to fail. A discovery that finds what the last one found is
	// not reported, so this is quiet between changes.
	OnDiscovery func(server string)

	// announcements carries backends that sent
	// notifications/tools/list_changed.
	announcements chan event.TypedGenericEvent[*mcpv1alpha1.MCPServer]
}

// NewCatalogReconciler builds a CatalogReconciler. Wire the registry's
// OnToolsChanged to its Announce.
func NewCatalogReconciler(c client.Client, registry *discovery.Registry, gw *Gateway) *CatalogReconciler {
	return &CatalogReconciler{
		Client:        c,
		Registry:      registry,
		Gateway:       gw,
		announcements: make(chan event.TypedGenericEvent[*mcpv1alpha1.MCPServer], 64),
	}
}

// Announce requests an immediate rediscovery of a backend. It never blocks: a
// backend announcing faster than the reconciler keeps up is re-read once.
func (r *CatalogReconciler) Announce(server string) {
	select {
	case r.announcements <- event.TypedGenericEvent[*mcpv1alpha1.MCPServer]{
		Object: &mcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: server}},
	}:
	default:
	}
}

// discoveryTimeout bounds one catalog read.
const discoveryTimeout = 15 * time.Second

func (r *CatalogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var server mcpv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if apierrors.IsNotFound(err) {
			r.Registry.Forget(req.Name)
			r.Gateway.Invalidate()
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !server.DeletionTimestamp.IsZero() {
		r.Registry.Forget(server.Name)
		r.Gateway.Invalidate()
		return ctrl.Result{}, nil
	}

	r.Registry.Ensure(server.Name, server.Spec.Endpoint, server.Spec.Auth)
	before, _ := r.Registry.Health(server.Name)
	discoverCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	catalog, changed, err := r.Registry.Discover(discoverCtx, server.Name)

	// Whatever happened, the tool lists may need to follow: a new catalog, a
	// catalog that just aged out, or one that is usable again.
	r.Gateway.Invalidate()
	if r.OnDiscovery != nil && (changed || (err != nil) != (before.Failures > 0)) {
		r.OnDiscovery(server.Name)
	}

	interval := server.Interval()
	if err != nil {
		_, health, _ := r.Registry.Catalog(server.Name)
		logger.Info("discovery failed", "server", server.Name, "failures", health.Failures, "error", err.Error())
		return ctrl.Result{RequeueAfter: min(discovery.RetryDelay(health.Failures), interval)}, nil
	}
	if changed {
		logger.Info("catalog changed", "server", server.Name, "revision", catalog.Revision, "tools", catalog.Len())
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *CatalogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPServer{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			catalogRevisionChanged{},
		))).
		WatchesRawSource(source.Channel(r.announcements, &handler.TypedEnqueueRequestForObject[*mcpv1alpha1.MCPServer]{})).
		WithOptions(controller.Options{
			NeedLeaderElection: ptr.To(false),
			// Backends are independent. One that takes its full timeout to
			// fail must not hold up discovery of the others.
			MaxConcurrentReconciles: 4,
		}).
		Named("catalog").
		Complete(r)
}

// catalogRevisionChanged passes updates that change status.catalogRevision:
// the controller has seen a catalog this replica may not have yet.
type catalogRevisionChanged struct {
	predicate.Funcs
}

func (catalogRevisionChanged) Update(e event.UpdateEvent) bool {
	oldServer, ok1 := e.ObjectOld.(*mcpv1alpha1.MCPServer)
	newServer, ok2 := e.ObjectNew.(*mcpv1alpha1.MCPServer)
	return ok1 && ok2 && oldServer.Status.CatalogRevision != newServer.Status.CatalogRevision
}

// PolicyReconciler rebuilds the policy snapshot whenever any of the four kinds
// changes.
//
// It does not reconcile objects one by one. Every event maps to the same key,
// so a burst of changes — a GitOps sync applying fifty grants — is collapsed by
// the work queue into one rebuild from the informer cache.
type PolicyReconciler struct {
	client.Client
	Gateway *Gateway
}

// policyKey is the one request every event maps to.
var policyKey = reconcile.Request{NamespacedName: types.NamespacedName{Name: "policy"}}

func (r *PolicyReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	var servers mcpv1alpha1.MCPServerList
	if err := r.List(ctx, &servers); err != nil {
		return ctrl.Result{}, err
	}
	var toolSets mcpv1alpha1.MCPToolSetList
	if err := r.List(ctx, &toolSets); err != nil {
		return ctrl.Result{}, err
	}
	var capabilities mcpv1alpha1.MCPCapabilityList
	if err := r.List(ctx, &capabilities); err != nil {
		return ctrl.Result{}, err
	}
	var grants mcpv1alpha1.MCPGrantList
	if err := r.List(ctx, &grants); err != nil {
		return ctrl.Result{}, err
	}
	r.Gateway.SetPolicy(policy.NewSnapshot(servers.Items, toolSets.Items, capabilities.Items, grants.Items))
	log.FromContext(ctx).V(1).Info("policy rebuilt",
		"servers", len(servers.Items), "toolSets", len(toolSets.Items),
		"capabilities", len(capabilities.Items), "grants", len(grants.Items))
	return ctrl.Result{}, nil
}

func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	toPolicy := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{policyKey}
	})
	// A cluster with none of the four kinds yet produces no event at all, and
	// the gateway would never become ready. One event at startup builds the
	// first snapshot regardless.
	initial := make(chan event.TypedGenericEvent[*mcpv1alpha1.MCPGrant], 1)
	initial <- event.TypedGenericEvent[*mcpv1alpha1.MCPGrant]{Object: &mcpv1alpha1.MCPGrant{}}
	onlySpec := builder.WithPredicates(predicate.GenerationChangedPredicate{})

	return ctrl.NewControllerManagedBy(mgr).
		Named("policy").
		Watches(&mcpv1alpha1.MCPServer{}, toPolicy, onlySpec).
		Watches(&mcpv1alpha1.MCPToolSet{}, toPolicy, onlySpec).
		Watches(&mcpv1alpha1.MCPCapability{}, toPolicy, onlySpec).
		Watches(&mcpv1alpha1.MCPGrant{}, toPolicy, onlySpec).
		WatchesRawSource(source.Channel(initial, handler.TypedEnqueueRequestsFromMapFunc(
			func(context.Context, *mcpv1alpha1.MCPGrant) []reconcile.Request { return []reconcile.Request{policyKey} },
		))).
		WithOptions(controller.Options{NeedLeaderElection: ptr.To(false)}).
		Complete(r)
}
