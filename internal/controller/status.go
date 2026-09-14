// Package controller holds the reconcilers of mcp-gate-controller.
//
// They verify and report. None of them is in the request path, and none of
// them changes what the gateway enforces: the gateway reads the same manifests
// and resolves them itself. What a reconciler writes into status is the
// controller's account of what the gateway will do with a resource, computed
// with the same resolution code — so an operator reading `kubectl get` sees the
// reason a tool is missing without reading gateway logs.
package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/metrics"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/policy"
)

// maxMessageLength keeps condition messages readable in kubectl. The API allows
// 32768 bytes; an error chain from a failed connection can approach that, and
// nobody reads past the first line of it anyway.
const maxMessageLength = 1024

// setCondition records a condition, keeping its transition time when the status
// did not change.
func setCondition(conditions *[]metav1.Condition, generation int64, conditionType string, status metav1.ConditionStatus, reason, message string) {
	if len(message) > maxMessageLength {
		message = message[:maxMessageLength-3] + "..."
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// isCurrentlyTrue reports whether a condition is True for the generation the
// object is at. A True recorded against an older spec says nothing about the
// current one.
func isCurrentlyTrue(conditions []metav1.Condition, conditionType string, generation int64) bool {
	c := meta.FindStatusCondition(conditions, conditionType)
	return c != nil && c.Status == metav1.ConditionTrue && c.ObservedGeneration == generation
}

// conditionSummary renders why a condition is not True, for a message on a
// resource that depends on it.
func conditionSummary(conditions []metav1.Condition, conditionType string) string {
	c := meta.FindStatusCondition(conditions, conditionType)
	if c == nil {
		return "not yet reconciled"
	}
	if c.Message == "" {
		return c.Reason
	}
	return c.Reason + ": " + c.Message
}

// patchStatus writes status if it changed. The patch is a merge patch without a
// resource version: the reconciler owns status outright, so there is nothing to
// conflict with, and a status that did not change costs no API call at all.
func patchStatus[T client.Object](ctx context.Context, c client.Client, obj T, base T) error {
	if equality.Semantic.DeepEqual(statusOf(base), statusOf(obj)) {
		return nil
	}
	return client.IgnoreNotFound(c.Status().Patch(ctx, obj, client.MergeFrom(base)))
}

func statusOf(obj client.Object) any {
	switch o := obj.(type) {
	case *gatev1alpha1.MCPServer:
		return o.Status
	case *gatev1alpha1.MCPToolSet:
		return o.Status
	case *gatev1alpha1.MCPCapability:
		return o.Status
	case *gatev1alpha1.MCPGrant:
		return o.Status
	}
	return nil
}

// conditionsOf returns the conditions of any of the four kinds.
func conditionsOf(obj client.Object) []metav1.Condition {
	switch o := obj.(type) {
	case *gatev1alpha1.MCPServer:
		return o.Status.Conditions
	case *gatev1alpha1.MCPToolSet:
		return o.Status.Conditions
	case *gatev1alpha1.MCPCapability:
		return o.Status.Conditions
	case *gatev1alpha1.MCPGrant:
		return o.Status.Conditions
	}
	return nil
}

// resourceTally is the Ready status of every object the reconcilers have seen,
// by kind and name, and the source of mcp_gate_resources.
//
// It is kept by the reconcilers rather than counted from a list of the cache.
// The cache has not yet seen the status a reconcile just wrote, and two
// reconciles of the same kind running side by side would each publish a tally
// with the other's object one step behind — the last to finish wins, and with
// both requeued on the same interval it stays wrong for good.
var resourceTally = struct {
	mu    sync.Mutex
	kinds map[string]map[string]metav1.ConditionStatus
}{kinds: map[string]map[string]metav1.ConditionStatus{}}

// observeResource records the Ready status of the object just reconciled, or
// forgets it when current is nil, and republishes the kind's tally.
func observeResource(kind, name string, current client.Object) {
	resourceTally.mu.Lock()
	defer resourceTally.mu.Unlock()
	byName, ok := resourceTally.kinds[kind]
	if !ok {
		byName = map[string]metav1.ConditionStatus{}
		resourceTally.kinds[kind] = byName
	}
	if current == nil {
		delete(byName, name)
	} else if c := meta.FindStatusCondition(conditionsOf(current), gatev1alpha1.ConditionReady); c != nil {
		byName[name] = c.Status
	} else {
		byName[name] = metav1.ConditionUnknown
	}

	byReady := map[string]int{}
	for _, status := range byName {
		byReady[string(status)]++
	}
	metrics.SetResources(kind, byReady)
}

// registryCatalogs gives policy resolution the tool names of the catalogs this
// controller has read, so that patterns — and the conflicts they can cause —
// resolve against the same backends the gateway serves.
func registryCatalogs(registry *discovery.Registry) policy.Catalogs {
	if registry == nil {
		return nil
	}
	return func(server string) ([]string, bool) {
		catalog, _, ok := registry.Catalog(server)
		if !ok {
			return nil, false
		}
		return catalog.Names(), true
	}
}

// loadSnapshot builds a policy snapshot from the cache, the same one the gateway
// builds from its own.
func loadSnapshot(ctx context.Context, c client.Client) (*policy.Snapshot, error) {
	var servers gatev1alpha1.MCPServerList
	if err := c.List(ctx, &servers); err != nil {
		return nil, err
	}
	var toolSets gatev1alpha1.MCPToolSetList
	if err := c.List(ctx, &toolSets); err != nil {
		return nil, err
	}
	var capabilities gatev1alpha1.MCPCapabilityList
	if err := c.List(ctx, &capabilities); err != nil {
		return nil, err
	}
	var grants gatev1alpha1.MCPGrantList
	if err := c.List(ctx, &grants); err != nil {
		return nil, err
	}
	return policy.NewSnapshot(servers.Items, toolSets.Items, capabilities.Items, grants.Items), nil
}

// conflictMessage describes name conflicts: which name, and which backend tools
// claim it.
func conflictMessage(conflicts map[string][]policy.ToolRef) string {
	names := make([]string, 0, len(conflicts))
	for name := range conflicts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		refs := make([]string, 0, len(conflicts[name]))
		for _, ref := range conflicts[name] {
			refs = append(refs, ref.String())
		}
		parts = append(parts, fmt.Sprintf("%q is exposed by %s", name, strings.Join(refs, " and ")))
	}
	return strings.Join(parts, "; ") + "; the gateway exposes none of them"
}

// quoted renders names for a message: "a", "b" and "c".
func quoted(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	switch len(q) {
	case 0:
		return ""
	case 1:
		return q[0]
	default:
		return strings.Join(q[:len(q)-1], ", ") + " and " + q[len(q)-1]
	}
}

// plural picks the verb form for a list of names.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
