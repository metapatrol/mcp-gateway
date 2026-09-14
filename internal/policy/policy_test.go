package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

func server(name string) gatev1alpha1.MCPServer {
	return gatev1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func toolSet(name, serverRef string, tools ...string) gatev1alpha1.MCPToolSet {
	return gatev1alpha1.MCPToolSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPToolSetSpec{ServerRef: serverRef, Tools: tools},
	}
}

func capability(name string, toolSets ...string) gatev1alpha1.MCPCapability {
	return gatev1alpha1.MCPCapability{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPCapabilitySpec{ToolSets: toolSets},
	}
}

func grantFor(name, subject string, capabilities ...string) gatev1alpha1.MCPGrant {
	return gatev1alpha1.MCPGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatev1alpha1.MCPGrantSpec{Subject: subject, Capabilities: capabilities},
	}
}

// concept is the example CONCEPT.md walks through: two backends, and a subject
// holding a subset of each.
func concept() *Snapshot {
	return NewSnapshot(
		[]gatev1alpha1.MCPServer{server("source-intelligence"), server("artifact")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("source-intelligence-analysis", "source-intelligence", "source_search", "source_get", "symbol_search"),
			toolSet("artifact-read", "artifact", "artifact_search", "artifact_get"),
			toolSet("artifact-readwrite", "artifact", "artifact_search", "artifact_get", "artifact_publish"),
		},
		[]gatev1alpha1.MCPCapability{
			capability("code-research", "source-intelligence-analysis", "artifact-read"),
			capability("artifact-authoring", "artifact-readwrite"),
		},
		[]gatev1alpha1.MCPGrant{
			grantFor("coding-agent", "coding-agent", "code-research"),
		},
	)
}

func TestResolve_ExposesOnlyTheGrantedSubset(t *testing.T) {
	res := concept().Resolve("coding-agent")

	assert.Equal(t, map[string]ToolRef{
		"source_search":   {Server: "source-intelligence", Tool: "source_search"},
		"source_get":      {Server: "source-intelligence", Tool: "source_get"},
		"symbol_search":   {Server: "source-intelligence", Tool: "symbol_search"},
		"artifact_search": {Server: "artifact", Tool: "artifact_search"},
		"artifact_get":    {Server: "artifact", Tool: "artifact_get"},
	}, res.Tools)
	assert.Empty(t, res.Conflicts)
	assert.NotContains(t, res.Tools, "artifact_publish", "a tool outside the grant must not be visible")
}

func TestResolve_UnknownSubjectGetsNothing(t *testing.T) {
	res := concept().Resolve("somebody-else")
	assert.Empty(t, res.Tools)
}

// The same backend tool reached through two tool sets is one tool. Treating it
// as a conflict would take away a tool the subject holds twice over.
func TestResolve_SameToolThroughTwoToolSetsIsNoConflict(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("artifact")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("artifact-read", "artifact", "artifact_get"),
			toolSet("artifact-readwrite", "artifact", "artifact_get", "artifact_publish"),
		},
		[]gatev1alpha1.MCPCapability{
			capability("read", "artifact-read"),
			capability("write", "artifact-readwrite"),
		},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "read", "write")},
	)

	res := s.Resolve("agent")
	assert.Empty(t, res.Conflicts)
	assert.Equal(t, ToolRef{Server: "artifact", Tool: "artifact_get"}, res.Tools["artifact_get"])
	assert.Len(t, res.Tools, 2)
}

// Two backends exposing the same name to one subject: neither is exposed, and
// the rest is unaffected.
func TestResolve_ConflictExposesNeither(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("engram"), server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("memory", "engram", "search", "remember"),
			toolSet("code", "source-intelligence", "search", "source_get"),
		},
		[]gatev1alpha1.MCPCapability{capability("both", "memory", "code")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "both")},
	)

	res := s.Resolve("agent")
	assert.NotContains(t, res.Tools, "search")
	assert.Equal(t, []ToolRef{
		{Server: "engram", Tool: "search"},
		{Server: "source-intelligence", Tool: "search"},
	}, res.Conflicts["search"])
	assert.Contains(t, res.Tools, "remember")
	assert.Contains(t, res.Tools, "source_get")
	assert.Equal(t, []string{"search"}, res.ConflictNames())
}

// Grants for one subject add up, and a conflict can arise between grants that
// are each fine on their own.
func TestResolve_GrantsForOneSubjectAddUp(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("a"), server("b")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("a-tools", "a", "one", "shared"),
			toolSet("b-tools", "b", "two", "shared"),
		},
		[]gatev1alpha1.MCPCapability{capability("ca", "a-tools"), capability("cb", "b-tools")},
		[]gatev1alpha1.MCPGrant{grantFor("first", "agent", "ca"), grantFor("second", "agent", "cb")},
	)

	res := s.Resolve("agent")
	assert.Contains(t, res.Tools, "one")
	assert.Contains(t, res.Tools, "two")
	assert.Contains(t, res.Conflicts, "shared")
}

// Fail closed: a reference that does not resolve contributes nothing, and it
// takes nothing away from what does resolve.
func TestResolve_UnresolvedReferencesNarrowNeverWiden(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("artifact")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("artifact-read", "artifact", "artifact_get"),
			toolSet("orphan", "gone", "anything"),
		},
		[]gatev1alpha1.MCPCapability{capability("research", "artifact-read", "orphan", "never-created")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "research", "typo")},
	)

	res := s.Resolve("agent")
	assert.Equal(t, map[string]ToolRef{"artifact_get": {Server: "artifact", Tool: "artifact_get"}}, res.Tools)
	assert.Equal(t, []string{"typo"}, res.MissingCapabilities)
	assert.Equal(t, []string{"never-created"}, res.MissingToolSets)
	assert.Equal(t, []string{"gone"}, res.MissingServers)
}

func TestResolve_ObjectsBeingDeletedGrantNothing(t *testing.T) {
	g := grantFor("g", "agent", "code-research")
	now := metav1.Now()
	g.DeletionTimestamp = &now

	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("artifact")},
		[]gatev1alpha1.MCPToolSet{toolSet("artifact-read", "artifact", "artifact_get")},
		[]gatev1alpha1.MCPCapability{capability("code-research", "artifact-read")},
		[]gatev1alpha1.MCPGrant{g},
	)
	assert.Empty(t, s.Resolve("agent").Tools)
}

func TestAuthorize(t *testing.T) {
	s := concept()

	ref, ok := s.Authorize("coding-agent", "artifact_get")
	assert.True(t, ok)
	assert.Equal(t, ToolRef{Server: "artifact", Tool: "artifact_get"}, ref)

	_, ok = s.Authorize("coding-agent", "artifact_publish")
	assert.False(t, ok, "a tool the backend has but the grant does not must be refused")

	_, ok = s.Authorize("", "artifact_get")
	assert.False(t, ok)
}

func TestEmptyGrantsNothing(t *testing.T) {
	assert.Empty(t, Empty().Resolve("anyone").Tools)
	assert.Empty(t, Empty().Subjects())
}

func TestSubjects(t *testing.T) {
	assert.Equal(t, []string{"coding-agent"}, concept().Subjects())
}
