package controller

import (
	"context"
	"fmt"
	"slices"

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

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// MCPGrantReconciler checks that a grant's capabilities exist, fit together, and
// are available — on their own and alongside the subject's other grants.
type MCPGrantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Registry holds the catalogs tool set patterns resolve against.
	Registry *discovery.Registry
}

// +kubebuilder:rbac:groups=mcp-gateway.metapatrol.com,resources=mcpgrants,verbs=get;list;watch
// +kubebuilder:rbac:groups=mcp-gateway.metapatrol.com,resources=mcpgrants/status,verbs=get;update;patch

func (r *MCPGrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var grant mcpv1alpha1.MCPGrant
	if err := r.Get(ctx, req.NamespacedName, &grant); err != nil {
		if apierrors.IsNotFound(err) {
			observeResource("MCPGrant", req.Name, nil)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	snapshot, err := loadSnapshot(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	base := grant.DeepCopy()
	gen := grant.Generation
	status := &grant.Status
	status.ObservedGeneration = gen
	catalogs := registryCatalogs(r.Registry)
	own := snapshot.ResolveCapabilities(grant.Spec.Capabilities, catalogs)

	switch {
	case len(own.MissingCapabilities) > 0:
		// Fail closed, and say so: the capabilities that do exist are still
		// granted, and the missing one grants nothing.
		msg := fmt.Sprintf("MCPCapability %s %s not exist and grant%s nothing",
			quoted(own.MissingCapabilities), plural(len(own.MissingCapabilities), "does", "do"), plural(len(own.MissingCapabilities), "s", ""))
		setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionValid, metav1.ConditionFalse, mcpv1alpha1.ReasonCapabilityNotFound, msg)
	case len(own.Conflicts) > 0:
		setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionValid, metav1.ConditionFalse, mcpv1alpha1.ReasonToolNameConflict, conflictMessage(own.Conflicts))
	default:
		setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionValid, metav1.ConditionTrue, mcpv1alpha1.ReasonResolved,
			fmt.Sprintf("%d %s resolve to %d %s", len(grant.Spec.Capabilities), plural(len(grant.Spec.Capabilities), "capability", "capabilities"),
				len(own.Tools), plural(len(own.Tools), "tool", "tools")))
	}

	switch {
	case !isCurrentlyTrue(status.Conditions, mcpv1alpha1.ConditionValid, gen):
		setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionReady, metav1.ConditionFalse, mcpv1alpha1.ReasonInvalid,
			"the grant is not valid")
	default:
		notReady, err := r.notReadyCapabilities(ctx, grant.Spec.Capabilities)
		if err != nil {
			return ctrl.Result{}, err
		}
		shared := sharedConflicts(snapshot.Resolve(grant.Spec.Subject, catalogs), own)
		switch {
		case len(notReady) > 0:
			setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionReady, metav1.ConditionFalse, mcpv1alpha1.ReasonCapabilityNotReady,
				joinReasons(notReady))
		case len(shared) > 0:
			// Each grant is fine on its own, and together they give the
			// subject two tools under one name. The gateway exposes neither,
			// so this grant does not deliver everything it names.
			setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionReady, metav1.ConditionFalse, mcpv1alpha1.ReasonToolNameConflict,
				fmt.Sprintf("together with the other grants for subject %q: %s", grant.Spec.Subject, conflictMessage(shared)))
		default:
			setCondition(&status.Conditions, gen, mcpv1alpha1.ConditionReady, metav1.ConditionTrue, mcpv1alpha1.ReasonAvailable,
				"all capabilities are ready")
		}
	}

	if err := patchStatus(ctx, r.Client, &grant, base); err != nil {
		return ctrl.Result{}, err
	}
	observeResource("MCPGrant", grant.Name, &grant)
	return ctrl.Result{}, nil
}

// sharedConflicts returns the conflicts of a subject's combined grants that
// involve a tool of this grant.
func sharedConflicts(subject, own policy.Resolution) map[string][]policy.ToolRef {
	out := map[string][]policy.ToolRef{}
	for name, refs := range subject.Conflicts {
		if _, mine := own.Tools[name]; mine {
			out[name] = refs
		}
	}
	return out
}

func (r *MCPGrantReconciler) notReadyCapabilities(ctx context.Context, names []string) (map[string]string, error) {
	notReady := map[string]string{}
	for _, name := range names {
		var capability mcpv1alpha1.MCPCapability
		if err := r.Get(ctx, types.NamespacedName{Name: name}, &capability); err != nil {
			if apierrors.IsNotFound(err) {
				notReady[name] = "does not exist"
				continue
			}
			return nil, err
		}
		if !isCurrentlyTrue(capability.Status.Conditions, mcpv1alpha1.ConditionReady, capability.Generation) {
			notReady[name] = conditionSummary(capability.Status.Conditions, mcpv1alpha1.ConditionReady)
		}
	}
	return notReady, nil
}

func (r *MCPGrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1alpha1.MCPGrant{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&mcpv1alpha1.MCPCapability{},
			handler.EnqueueRequestsFromMapFunc(r.grantsForCapability),
			builder.WithPredicates(dependencyChanged{}),
		).
		// A tool set gaining a tool — by a spec change, or by a pattern
		// matching a tool the backend added — can create a conflict between
		// capabilities that each still resolve cleanly, so no capability
		// status changes and the watch above never fires.
		Watches(
			&mcpv1alpha1.MCPToolSet{},
			handler.EnqueueRequestsFromMapFunc(r.allGrants),
			builder.WithPredicates(toolSetSelectionChanged{}),
		).
		// Grants for one subject share a tool namespace: a change to one can
		// create or clear a conflict in the others.
		Watches(
			&mcpv1alpha1.MCPGrant{},
			handler.EnqueueRequestsFromMapFunc(r.grantsForSameSubject),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("mcpgrant").
		Complete(r)
}

func (r *MCPGrantReconciler) grantsForCapability(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.grantsWhere(ctx, func(g *mcpv1alpha1.MCPGrant) bool {
		return slices.Contains(g.Spec.Capabilities, obj.GetName())
	})
}

func (r *MCPGrantReconciler) allGrants(ctx context.Context, _ client.Object) []reconcile.Request {
	return r.grantsWhere(ctx, func(*mcpv1alpha1.MCPGrant) bool { return true })
}

func (r *MCPGrantReconciler) grantsForSameSubject(ctx context.Context, obj client.Object) []reconcile.Request {
	changed, ok := obj.(*mcpv1alpha1.MCPGrant)
	if !ok {
		return nil
	}
	return r.grantsWhere(ctx, func(g *mcpv1alpha1.MCPGrant) bool {
		return g.Name != changed.Name && g.Spec.Subject == changed.Spec.Subject
	})
}

func (r *MCPGrantReconciler) grantsWhere(ctx context.Context, match func(*mcpv1alpha1.MCPGrant) bool) []reconcile.Request {
	var list mcpv1alpha1.MCPGrantList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var requests []reconcile.Request
	for i := range list.Items {
		if match(&list.Items[i]) {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
		}
	}
	return requests
}
