package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
)

// requeueCatalogPending is how soon a tool set is verified again when its server
// is Ready but this process has not read the catalog yet — which is the case
// for every tool set in the seconds after the controller starts.
const requeueCatalogPending = 5 * time.Second

// MCPToolSetReconciler verifies each tool set against its server's catalog.
type MCPToolSetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Registry *discovery.Registry
}

// +kubebuilder:rbac:groups=mcp.gate.metapatrol.com,resources=mcptoolsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.gate.metapatrol.com,resources=mcptoolsets/status,verbs=get;update;patch

func (r *MCPToolSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var toolSet gatev1alpha1.MCPToolSet
	if err := r.Get(ctx, req.NamespacedName, &toolSet); err != nil {
		if apierrors.IsNotFound(err) {
			observeResource("MCPToolSet", req.Name, nil)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	base := toolSet.DeepCopy()
	result, err := r.verify(ctx, &toolSet)
	if err != nil {
		return ctrl.Result{}, err
	}
	toolSet.Status.ObservedGeneration = toolSet.Generation
	if err := patchStatus(ctx, r.Client, &toolSet, base); err != nil {
		return ctrl.Result{}, err
	}
	observeResource("MCPToolSet", toolSet.Name, &toolSet)
	return result, nil
}

func (r *MCPToolSetReconciler) verify(ctx context.Context, toolSet *gatev1alpha1.MCPToolSet) (ctrl.Result, error) {
	gen := toolSet.Generation
	status := &toolSet.Status
	serverName := toolSet.Spec.ServerRef

	var server gatev1alpha1.MCPServer
	if err := r.Get(ctx, types.NamespacedName{Name: serverName}, &server); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		msg := fmt.Sprintf("MCPServer %q does not exist", serverName)
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionValid, metav1.ConditionFalse, gatev1alpha1.ReasonServerNotFound, msg)
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionFalse, gatev1alpha1.ReasonInvalid, msg)
		status.MissingTools = nil
		status.CatalogRevision = ""
		return ctrl.Result{}, nil
	}
	setCondition(&status.Conditions, gen, gatev1alpha1.ConditionValid, metav1.ConditionTrue, gatev1alpha1.ReasonResolved,
		fmt.Sprintf("MCPServer %q exists", serverName))

	if !isCurrentlyTrue(server.Status.Conditions, gatev1alpha1.ConditionReady, server.Generation) {
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionFalse, gatev1alpha1.ReasonServerUnavailable,
			fmt.Sprintf("MCPServer %q is not ready: %s", serverName, conditionSummary(server.Status.Conditions, gatev1alpha1.ConditionReady)))
		return ctrl.Result{}, nil
	}

	catalog, _, ok := r.Registry.Catalog(serverName)
	if !ok {
		// The server reports Ready, so some earlier process read its catalog;
		// this one has not yet. Keep what an earlier verification concluded
		// rather than flap to Unknown on every restart.
		if meta.FindStatusCondition(status.Conditions, gatev1alpha1.ConditionReady) == nil {
			setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionUnknown, gatev1alpha1.ReasonCatalogPending,
				fmt.Sprintf("the catalog of MCPServer %q has not been read yet", serverName))
		}
		return ctrl.Result{RequeueAfter: requeueCatalogPending}, nil
	}

	var missing []string
	for _, tool := range toolSet.Spec.Tools {
		if _, ok := catalog.Tool(tool); !ok {
			missing = append(missing, tool)
		}
	}
	status.CatalogRevision = catalog.Revision
	status.MissingTools = missing
	if len(missing) > 0 {
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionFalse, gatev1alpha1.ReasonMissingTools,
			fmt.Sprintf("%s %s not currently exposed by %s", quoted(missing), plural(len(missing), "is", "are"), serverName))
		return ctrl.Result{}, nil
	}
	setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable,
		fmt.Sprintf("all %d %s exposed by %s", len(toolSet.Spec.Tools), plural(len(toolSet.Spec.Tools), "tool is", "tools are"), serverName))
	return ctrl.Result{}, nil
}

func (r *MCPToolSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatev1alpha1.MCPToolSet{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&gatev1alpha1.MCPServer{},
			handler.EnqueueRequestsFromMapFunc(r.toolSetsForServer),
			builder.WithPredicates(serverStateChanged{}),
		).
		Named("mcptoolset").
		Complete(r)
}

// toolSetsForServer enqueues the tool sets that select from a server.
func (r *MCPToolSetReconciler) toolSetsForServer(ctx context.Context, obj client.Object) []reconcile.Request {
	var list gatev1alpha1.MCPToolSetList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		if list.Items[i].Spec.ServerRef == obj.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return requests
}

// serverStateChanged passes the server updates a tool set's verdict can depend
// on: a new spec, a new catalog, or a change in readiness. It drops the rest —
// above all the status write every discovery makes to lastDiscoveryTime.
type serverStateChanged struct {
	predicate.Funcs
}

func (serverStateChanged) Update(e event.UpdateEvent) bool {
	oldServer, ok1 := e.ObjectOld.(*gatev1alpha1.MCPServer)
	newServer, ok2 := e.ObjectNew.(*gatev1alpha1.MCPServer)
	if !ok1 || !ok2 {
		return false
	}
	return oldServer.Generation != newServer.Generation ||
		oldServer.Status.CatalogRevision != newServer.Status.CatalogRevision ||
		oldServer.Status.Available != newServer.Status.Available ||
		conditionChanged(oldServer.Status.Conditions, newServer.Status.Conditions, gatev1alpha1.ConditionReady)
}

// dependencyChanged passes updates to a dependency's spec or to its Valid or
// Ready conditions — what the resources built on it report about it.
type dependencyChanged struct {
	predicate.Funcs
}

func (dependencyChanged) Update(e event.UpdateEvent) bool {
	if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
		return true
	}
	oldConditions, newConditions := conditionsOf(e.ObjectOld), conditionsOf(e.ObjectNew)
	return conditionChanged(oldConditions, newConditions, gatev1alpha1.ConditionValid) ||
		conditionChanged(oldConditions, newConditions, gatev1alpha1.ConditionReady)
}

func conditionChanged(oldConditions, newConditions []metav1.Condition, conditionType string) bool {
	o := meta.FindStatusCondition(oldConditions, conditionType)
	n := meta.FindStatusCondition(newConditions, conditionType)
	if o == nil || n == nil {
		return o != n
	}
	return o.Status != n.Status || o.Reason != n.Reason || o.Message != n.Message || o.ObservedGeneration != n.ObservedGeneration
}
