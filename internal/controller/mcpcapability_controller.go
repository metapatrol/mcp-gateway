package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
)

// MCPCapabilityReconciler checks that a capability's tool sets exist, fit
// together, and are available.
type MCPCapabilityReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Registry holds the catalogs tool set patterns resolve against.
	Registry *discovery.Registry
}

// +kubebuilder:rbac:groups=mcp.gate.metapatrol.com,resources=mcpcapabilities,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp.gate.metapatrol.com,resources=mcpcapabilities/status,verbs=get;update;patch

func (r *MCPCapabilityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var capability gatev1alpha1.MCPCapability
	if err := r.Get(ctx, req.NamespacedName, &capability); err != nil {
		if apierrors.IsNotFound(err) {
			observeResource("MCPCapability", req.Name, nil)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	snapshot, err := loadSnapshot(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	base := capability.DeepCopy()
	gen := capability.Generation
	status := &capability.Status
	status.ObservedGeneration = gen
	res := snapshot.ResolveToolSets(capability.Spec.ToolSets, registryCatalogs(r.Registry))

	switch {
	case len(res.MissingToolSets) > 0:
		msg := fmt.Sprintf("MCPToolSet %s %s not exist", quoted(res.MissingToolSets), plural(len(res.MissingToolSets), "does", "do"))
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionValid, metav1.ConditionFalse, gatev1alpha1.ReasonToolSetNotFound, msg)
	case len(res.Conflicts) > 0:
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionValid, metav1.ConditionFalse, gatev1alpha1.ReasonToolNameConflict, conflictMessage(res.Conflicts))
	default:
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionValid, metav1.ConditionTrue, gatev1alpha1.ReasonResolved,
			fmt.Sprintf("%d %s resolve to %d %s", len(capability.Spec.ToolSets), plural(len(capability.Spec.ToolSets), "tool set", "tool sets"),
				len(res.Tools), plural(len(res.Tools), "tool", "tools")))
	}

	if !isCurrentlyTrue(status.Conditions, gatev1alpha1.ConditionValid, gen) {
		setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionFalse, gatev1alpha1.ReasonInvalid,
			"the capability is not valid")
	} else {
		notReady, err := r.notReadyToolSets(ctx, capability.Spec.ToolSets)
		if err != nil {
			return ctrl.Result{}, err
		}
		if len(notReady) > 0 {
			setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionFalse, gatev1alpha1.ReasonToolSetNotReady,
				joinReasons(notReady))
		} else {
			setCondition(&status.Conditions, gen, gatev1alpha1.ConditionReady, metav1.ConditionTrue, gatev1alpha1.ReasonAvailable,
				"all tool sets are ready")
		}
	}

	if err := patchStatus(ctx, r.Client, &capability, base); err != nil {
		return ctrl.Result{}, err
	}
	observeResource("MCPCapability", capability.Name, &capability)
	return ctrl.Result{}, nil
}

// notReadyToolSets returns, per tool set that is not Ready, why not.
func (r *MCPCapabilityReconciler) notReadyToolSets(ctx context.Context, names []string) (map[string]string, error) {
	notReady := map[string]string{}
	for _, name := range names {
		var toolSet gatev1alpha1.MCPToolSet
		if err := r.Get(ctx, types.NamespacedName{Name: name}, &toolSet); err != nil {
			if apierrors.IsNotFound(err) {
				notReady[name] = "does not exist"
				continue
			}
			return nil, err
		}
		if !isCurrentlyTrue(toolSet.Status.Conditions, gatev1alpha1.ConditionReady, toolSet.Generation) {
			notReady[name] = conditionSummary(toolSet.Status.Conditions, gatev1alpha1.ConditionReady)
		}
	}
	return notReady, nil
}

func (r *MCPCapabilityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatev1alpha1.MCPCapability{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&gatev1alpha1.MCPToolSet{},
			handler.EnqueueRequestsFromMapFunc(r.capabilitiesForToolSet),
			builder.WithPredicates(dependencyChanged{}),
		).
		Named("mcpcapability").
		Complete(r)
}

// capabilitiesForToolSet enqueues the capabilities that name a tool set.
func (r *MCPCapabilityReconciler) capabilitiesForToolSet(ctx context.Context, obj client.Object) []reconcile.Request {
	var list gatev1alpha1.MCPCapabilityList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		if slices.Contains(list.Items[i].Spec.ToolSets, obj.GetName()) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return requests
}

// joinReasons renders name-to-reason pairs in name order.
func joinReasons(reasons map[string]string) string {
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	sort.Strings(names)
	msg := ""
	for i, name := range names {
		if i > 0 {
			msg += "; "
		}
		msg += fmt.Sprintf("%q %s", name, reasons[name])
	}
	return msg
}
