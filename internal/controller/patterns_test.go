package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

var siTools = []string{
	"code.architecture", "code.inventory", "code.search", "code.symbol", "code.trace",
	"refs.list", "repositories.get", "repositories.list", "repositories.list_files", "repositories.read_file",
}

// What a tool set with patterns selects is in its status, so `kubectl get`
// answers "what does this actually grant?".
func TestMCPToolSetReconciler_PatternsResolveIntoStatus(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", siTools...)
	ts := mcpToolSet("research", "source-intelligence", "code.*", "repositories.*")
	ts.Spec.Exclude = []string{"repositories.read_file", "code.trace"}
	c := newFakeClient(readyServer("source-intelligence"), ts)
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "research")

	got := get(t, c, "research", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, []string{
		"code.architecture", "code.inventory", "code.search", "code.symbol",
		"repositories.get", "repositories.list", "repositories.list_files",
	}, got.Status.ResolvedTools)
	assert.Equal(t, 7, got.Status.ResolvedToolCount)
	ready := condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
	assert.Equal(t, "7 tools selected from source-intelligence", ready.Message)
}

// A wildcard that selects nothing is almost always a typo, and a tool set that
// silently grants nothing is the failure hardest to notice.
func TestMCPToolSetReconciler_UnmatchedPattern(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", siTools...)
	c := newFakeClient(readyServer("source-intelligence"), mcpToolSet("research", "source-intelligence", "cod.*", "refs.list"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "research")

	got := get(t, c, "research", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, []string{"cod.*"}, got.Status.UnmatchedPatterns)
	assert.Equal(t, []string{"refs.list"}, got.Status.ResolvedTools)
	ready := condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, gatev1alpha1.ReasonUnmatchedPatterns, ready.Reason)
	assert.Equal(t, `"cod.*" selects no tool of source-intelligence`, ready.Message)
}

func TestMCPToolSetReconciler_MissingNameAndUnmatchedPatternTogether(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", siTools...)
	c := newFakeClient(readyServer("source-intelligence"), mcpToolSet("research", "source-intelligence", "symbol_search", "cod.*"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "research")

	ready := condition(t, get(t, c, "research", &gatev1alpha1.MCPToolSet{}).Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, gatev1alpha1.ReasonMissingTools, ready.Reason)
	assert.Equal(t, `"symbol_search" is not currently exposed by source-intelligence; "cod.*" selects no tool of source-intelligence`, ready.Message)
}

// A conflict between patterns exists only against the catalogs, so the
// capability reconciler resolves against them to report it.
func TestMCPCapabilityReconciler_ConflictBetweenPatterns(t *testing.T) {
	engram := mcptest.NewBackend(t, "engram", "remember", "code.search")
	si := mcptest.NewBackend(t, "source-intelligence", siTools...)
	c := newFakeClient(readyServer("engram"), readyServer("source-intelligence"),
		withReady(mcpToolSet("memory", "engram", "*"), true),
		withReady(mcpToolSet("code", "source-intelligence", "code.*"), true),
		mcpCapability("both", "memory", "code"))
	registry := newTestRegistry(t)
	toolSets := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: registry}
	discovered(t, toolSets, engram)
	discovered(t, toolSets, si)

	reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme(), Registry: registry}, "both")

	valid := condition(t, get(t, c, "both", &gatev1alpha1.MCPCapability{}).Status.Conditions, gatev1alpha1.ConditionValid)
	assert.Equal(t, gatev1alpha1.ReasonToolNameConflict, valid.Reason)
	assert.Equal(t, `"code.search" is exposed by engram/code.search and source-intelligence/code.search; the gateway exposes none of them`, valid.Message)
}

// A tool set that renames tools shows the names agents get, and which backend
// tool each renamed one is.
func TestMCPToolSetReconciler_RenamesResolveIntoStatus(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", siTools...)
	ts := mcpToolSet("research", "source-intelligence", "code.search", "refs.list")
	ts.Spec.Prefix = "si_"
	ts.Spec.Aliases = map[string]gatev1alpha1.ToolName{"code.search": "search"}
	c := newFakeClient(readyServer("source-intelligence"), ts)
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "research")

	got := get(t, c, "research", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, []string{"search", "si_refs.list"}, got.Status.ResolvedTools)
	assert.Equal(t, map[string]string{"search": "code.search", "si_refs.list": "refs.list"}, got.Status.BackendNames)
	assert.Empty(t, got.Status.UnusedAliases)
	assert.Equal(t, metav1.ConditionTrue, condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady).Status)
}

// An alias for a tool the set does not select renames nothing, which is almost
// always a slip — a typo in the key, or an exclusion that took the tool away.
func TestMCPToolSetReconciler_UnusedAlias(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", siTools...)
	ts := mcpToolSet("research", "source-intelligence", "code.*")
	ts.Spec.Aliases = map[string]gatev1alpha1.ToolName{"code.serach": "search"}
	c := newFakeClient(readyServer("source-intelligence"), ts)
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "research")

	got := get(t, c, "research", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, []string{"code.serach"}, got.Status.UnusedAliases)
	ready := condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, gatev1alpha1.ReasonUnusedAliases, ready.Reason)
	assert.Equal(t, `"code.serach" has an alias but not selected`, ready.Message)
}
