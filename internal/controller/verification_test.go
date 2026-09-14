package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gate/internal/mcptest"
)

// discovered registers a real backend and reads its catalog, so the tool set
// reconciler has something to verify against.
func discovered(t *testing.T, r *MCPToolSetReconciler, backend *mcptest.Backend) {
	t.Helper()
	r.Registry.Ensure(backend.Name, backend.URL)
	_, _, err := r.Registry.Discover(context.Background(), backend.Name)
	require.NoError(t, err)
}

func TestMCPToolSetReconciler_AllToolsPresent(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", "source_search", "source_get", "symbol_search")
	c := newFakeClient(readyServer("source-intelligence"),
		mcpToolSet("analysis", "source-intelligence", "source_search", "source_get", "symbol_search"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "analysis")

	ts := get(t, c, "analysis", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, metav1.ConditionTrue, condition(t, ts.Status.Conditions, gatev1alpha1.ConditionValid).Status)
	assert.Equal(t, metav1.ConditionTrue, condition(t, ts.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	assert.Empty(t, ts.Status.MissingTools)
	assert.NotEmpty(t, ts.Status.CatalogRevision)
}

// The example from CONCEPT.md, word for word in the message.
func TestMCPToolSetReconciler_MissingTool(t *testing.T) {
	backend := mcptest.NewBackend(t, "source-intelligence", "source_search", "source_get")
	c := newFakeClient(readyServer("source-intelligence"),
		mcpToolSet("analysis", "source-intelligence", "source_search", "source_get", "symbol_search"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}
	discovered(t, r, backend)

	reconcileNamed(t, r, "analysis")

	ts := get(t, c, "analysis", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, metav1.ConditionTrue, condition(t, ts.Status.Conditions, gatev1alpha1.ConditionValid).Status)
	ready := condition(t, ts.Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, gatev1alpha1.ReasonMissingTools, ready.Reason)
	assert.Equal(t, `"symbol_search" is not currently exposed by source-intelligence`, ready.Message)
	assert.Equal(t, []string{"symbol_search"}, ts.Status.MissingTools)
}

// invalid CRD configuration -> Valid=False
func TestMCPToolSetReconciler_ServerDoesNotExist(t *testing.T) {
	c := newFakeClient(mcpToolSet("analysis", "typo", "source_search"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}

	reconcileNamed(t, r, "analysis")

	ts := get(t, c, "analysis", &gatev1alpha1.MCPToolSet{})
	valid := condition(t, ts.Status.Conditions, gatev1alpha1.ConditionValid)
	assert.Equal(t, metav1.ConditionFalse, valid.Status)
	assert.Equal(t, gatev1alpha1.ReasonServerNotFound, valid.Reason)
	assert.Equal(t, metav1.ConditionFalse, condition(t, ts.Status.Conditions, gatev1alpha1.ConditionReady).Status)
}

// backend unavailable -> Valid=True, Ready=False
func TestMCPToolSetReconciler_ServerUnavailable(t *testing.T) {
	server := mcpServer("source-intelligence", "http://x/mcp")
	server.Status.Conditions = []metav1.Condition{{
		Type: gatev1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: gatev1alpha1.ReasonDiscoveryFailed,
		Message: "connection refused", ObservedGeneration: 1,
	}}
	c := newFakeClient(server, mcpToolSet("analysis", "source-intelligence", "source_search"))
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}

	reconcileNamed(t, r, "analysis")

	ts := get(t, c, "analysis", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, metav1.ConditionTrue, condition(t, ts.Status.Conditions, gatev1alpha1.ConditionValid).Status)
	ready := condition(t, ts.Status.Conditions, gatev1alpha1.ConditionReady)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, gatev1alpha1.ReasonServerUnavailable, ready.Reason)
	assert.Contains(t, ready.Message, "connection refused")
}

// Right after the controller starts, servers are Ready but no catalog has been
// read in this process yet. That is a wait, not a verdict.
func TestMCPToolSetReconciler_CatalogPendingKeepsThePreviousVerdict(t *testing.T) {
	ts := withReady(mcpToolSet("analysis", "source-intelligence", "source_search"), true)
	c := newFakeClient(readyServer("source-intelligence"), ts)
	r := &MCPToolSetReconciler{Client: c, Scheme: c.Scheme(), Registry: newTestRegistry(t)}

	res := reconcileNamed(t, r, "analysis")
	assert.Equal(t, requeueCatalogPending, res.RequeueAfter)
	got := get(t, c, "analysis", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, metav1.ConditionTrue, condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady).Status)

	fresh := mcpToolSet("fresh", "source-intelligence", "source_search")
	require.NoError(t, c.Create(context.Background(), fresh))
	reconcileNamed(t, r, "fresh")
	got = get(t, c, "fresh", &gatev1alpha1.MCPToolSet{})
	assert.Equal(t, metav1.ConditionUnknown, condition(t, got.Status.Conditions, gatev1alpha1.ConditionReady).Status)
}

func TestMCPCapabilityReconciler(t *testing.T) {
	t.Run("all tool sets ready", func(t *testing.T) {
		c := newFakeClient(readyServer("a"), readyServer("b"),
			withReady(mcpToolSet("a-tools", "a", "one"), true),
			withReady(mcpToolSet("b-tools", "b", "two"), true),
			mcpCapability("both", "a-tools", "b-tools"))
		reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}, "both")

		cp := get(t, c, "both", &gatev1alpha1.MCPCapability{})
		assert.Equal(t, metav1.ConditionTrue, condition(t, cp.Status.Conditions, gatev1alpha1.ConditionValid).Status)
		assert.Equal(t, metav1.ConditionTrue, condition(t, cp.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	})

	t.Run("a tool set does not exist", func(t *testing.T) {
		c := newFakeClient(readyServer("a"), withReady(mcpToolSet("a-tools", "a", "one"), true),
			mcpCapability("both", "a-tools", "missing"))
		reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}, "both")

		cp := get(t, c, "both", &gatev1alpha1.MCPCapability{})
		valid := condition(t, cp.Status.Conditions, gatev1alpha1.ConditionValid)
		assert.Equal(t, metav1.ConditionFalse, valid.Status)
		assert.Equal(t, gatev1alpha1.ReasonToolSetNotFound, valid.Reason)
		assert.Contains(t, valid.Message, `"missing"`)
		assert.Equal(t, gatev1alpha1.ReasonInvalid, condition(t, cp.Status.Conditions, gatev1alpha1.ConditionReady).Reason)
	})

	t.Run("a tool set is not ready", func(t *testing.T) {
		c := newFakeClient(readyServer("a"), readyServer("b"),
			withReady(mcpToolSet("a-tools", "a", "one"), true),
			withReady(mcpToolSet("b-tools", "b", "two"), false),
			mcpCapability("both", "a-tools", "b-tools"))
		reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}, "both")

		cp := get(t, c, "both", &gatev1alpha1.MCPCapability{})
		assert.Equal(t, metav1.ConditionTrue, condition(t, cp.Status.Conditions, gatev1alpha1.ConditionValid).Status)
		ready := condition(t, cp.Status.Conditions, gatev1alpha1.ConditionReady)
		assert.Equal(t, gatev1alpha1.ReasonToolSetNotReady, ready.Reason)
		assert.Contains(t, ready.Message, `"b-tools"`)
		assert.NotContains(t, ready.Message, `"a-tools"`)
	})

	t.Run("two servers claim one name", func(t *testing.T) {
		c := newFakeClient(readyServer("engram"), readyServer("source-intelligence"),
			withReady(mcpToolSet("memory", "engram", "search"), true),
			withReady(mcpToolSet("code", "source-intelligence", "search"), true),
			mcpCapability("both", "memory", "code"))
		reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}, "both")

		cp := get(t, c, "both", &gatev1alpha1.MCPCapability{})
		valid := condition(t, cp.Status.Conditions, gatev1alpha1.ConditionValid)
		assert.Equal(t, gatev1alpha1.ReasonToolNameConflict, valid.Reason)
		assert.Equal(t, `"search" is exposed by engram/search and source-intelligence/search; the gateway exposes none of them`, valid.Message)
	})

	// A Ready recorded against an older spec says nothing about the current one.
	t.Run("a stale Ready does not count", func(t *testing.T) {
		ts := withReady(mcpToolSet("a-tools", "a", "one"), true)
		ts.Generation = 2
		c := newFakeClient(readyServer("a"), ts, mcpCapability("cap", "a-tools"))
		reconcileNamed(t, &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}, "cap")

		cp := get(t, c, "cap", &gatev1alpha1.MCPCapability{})
		assert.Equal(t, metav1.ConditionFalse, condition(t, cp.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	})
}

func TestMCPGrantReconciler(t *testing.T) {
	base := func() []client.Object {
		return []client.Object{
			readyServer("a"), readyServer("b"),
			withReady(mcpToolSet("a-tools", "a", "one", "shared"), true),
			withReady(mcpToolSet("b-tools", "b", "two", "shared"), true),
			withReady(mcpCapability("ca", "a-tools"), true),
			withReady(mcpCapability("cb", "b-tools"), true),
		}
	}

	t.Run("ready", func(t *testing.T) {
		c := newFakeClient(objects(base(), mcpGrant("g", "agent", "ca"))...)
		reconcileNamed(t, &MCPGrantReconciler{Client: c, Scheme: c.Scheme()}, "g")

		g := get(t, c, "g", &gatev1alpha1.MCPGrant{})
		assert.Equal(t, metav1.ConditionTrue, condition(t, g.Status.Conditions, gatev1alpha1.ConditionValid).Status)
		assert.Equal(t, metav1.ConditionTrue, condition(t, g.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	})

	// Invalid grant: a grant referencing a missing capability must report
	// invalid status.
	t.Run("a capability does not exist", func(t *testing.T) {
		c := newFakeClient(objects(base(), mcpGrant("g", "agent", "ca", "typo"))...)
		reconcileNamed(t, &MCPGrantReconciler{Client: c, Scheme: c.Scheme()}, "g")

		g := get(t, c, "g", &gatev1alpha1.MCPGrant{})
		valid := condition(t, g.Status.Conditions, gatev1alpha1.ConditionValid)
		assert.Equal(t, metav1.ConditionFalse, valid.Status)
		assert.Equal(t, gatev1alpha1.ReasonCapabilityNotFound, valid.Reason)
		assert.Equal(t, `MCPCapability "typo" does not exist and grants nothing`, valid.Message)
		assert.Equal(t, metav1.ConditionFalse, condition(t, g.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	})

	t.Run("its own capabilities conflict", func(t *testing.T) {
		c := newFakeClient(objects(base(), mcpGrant("g", "agent", "ca", "cb"))...)
		reconcileNamed(t, &MCPGrantReconciler{Client: c, Scheme: c.Scheme()}, "g")

		g := get(t, c, "g", &gatev1alpha1.MCPGrant{})
		assert.Equal(t, gatev1alpha1.ReasonToolNameConflict, condition(t, g.Status.Conditions, gatev1alpha1.ConditionValid).Reason)
	})

	// Two grants, each valid on its own, give one subject two tools under one
	// name. Both report it.
	t.Run("a conflict with another grant for the same subject", func(t *testing.T) {
		c := newFakeClient(objects(base(), mcpGrant("first", "agent", "ca"), mcpGrant("second", "agent", "cb"), mcpGrant("other", "someone-else", "cb"))...)
		r := &MCPGrantReconciler{Client: c, Scheme: c.Scheme()}
		for _, name := range []string{"first", "second", "other"} {
			reconcileNamed(t, r, name)
		}

		for _, name := range []string{"first", "second"} {
			g := get(t, c, name, &gatev1alpha1.MCPGrant{})
			assert.Equal(t, metav1.ConditionTrue, condition(t, g.Status.Conditions, gatev1alpha1.ConditionValid).Status, name)
			ready := condition(t, g.Status.Conditions, gatev1alpha1.ConditionReady)
			assert.Equal(t, gatev1alpha1.ReasonToolNameConflict, ready.Reason, name)
			assert.Contains(t, ready.Message, `"shared"`)
		}
		other := get(t, c, "other", &gatev1alpha1.MCPGrant{})
		assert.Equal(t, metav1.ConditionTrue, condition(t, other.Status.Conditions, gatev1alpha1.ConditionReady).Status)
	})

	t.Run("a capability is not ready", func(t *testing.T) {
		objs := base()
		objs[5] = withReady(mcpCapability("cb", "b-tools"), false)
		c := newFakeClient(objects(objs, mcpGrant("g", "agent", "cb"))...)
		reconcileNamed(t, &MCPGrantReconciler{Client: c, Scheme: c.Scheme()}, "g")

		g := get(t, c, "g", &gatev1alpha1.MCPGrant{})
		assert.Equal(t, gatev1alpha1.ReasonCapabilityNotReady, condition(t, g.Status.Conditions, gatev1alpha1.ConditionReady).Reason)
	})
}

func TestStatusIsNotRewrittenWhenNothingChanged(t *testing.T) {
	c := newFakeClient(readyServer("a"), withReady(mcpToolSet("a-tools", "a", "one"), true), mcpCapability("cap", "a-tools"))
	r := &MCPCapabilityReconciler{Client: c, Scheme: c.Scheme()}
	reconcileNamed(t, r, "cap")
	first := get(t, c, "cap", &gatev1alpha1.MCPCapability{})

	reconcileNamed(t, r, "cap")
	second := get(t, c, "cap", &gatev1alpha1.MCPCapability{})
	assert.Equal(t, first.ResourceVersion, second.ResourceVersion)
}

func objects(items []client.Object, more ...client.Object) []client.Object {
	return append(items, more...)
}
