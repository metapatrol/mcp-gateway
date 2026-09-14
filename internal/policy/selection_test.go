package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"code.search", "code.search", true},
		{"code.search", "code.searcher", false},
		{"code.*", "code.search", true},
		{"code.*", "code.", true},
		{"code.*", "code", false},
		{"code.*", "xcode.search", false}, // anchored at both ends
		{"*", "anything.at_all-here", true},
		{"*", "", true},
		{"*search*", "code.search", true},
		{"*search*", "search", true},
		{"artifact_publish*", "artifact_publish_chart", true},
		{"artifact_publish*", "artifact_publis", false},
		{"repositories.?ist", "repositories.list", true},
		{"repositories.?ist", "repositories.lst", false},
		{"a*b*c", "a-x-b-y-c", true},
		{"a*b*c", "a-x-c-y-b", false},
		{"Code.*", "code.search", false}, // case-sensitive, as tool names are
		{"*.*.*", "a.b.c", true},
		{"*.*.*", "a.b", false},
	} {
		assert.Equal(t, tc.want, Match(tc.pattern, tc.name), "%q ~ %q", tc.pattern, tc.name)
	}
}

func TestIsPattern(t *testing.T) {
	assert.False(t, IsPattern("code.search"))
	assert.True(t, IsPattern("code.*"))
	assert.True(t, IsPattern("repositories.?ist"))
}

var siCatalog = []string{
	"code.architecture", "code.inventory", "code.search", "code.symbol", "code.trace",
	"refs.list", "repositories.get", "repositories.list", "repositories.list_files", "repositories.read_file",
}

func spec(tools []string, exclude ...string) gatev1alpha1.MCPToolSetSpec {
	return gatev1alpha1.MCPToolSetSpec{ServerRef: "source-intelligence", Tools: tools, Exclude: exclude}
}

func TestSelect(t *testing.T) {
	t.Run("patterns and names together", func(t *testing.T) {
		sel := Select(spec([]string{"code.*", "refs.list"}), siCatalog)
		assert.Equal(t, []string{"code.architecture", "code.inventory", "code.search", "code.symbol", "code.trace", "refs.list"}, sel.Tools)
		assert.Empty(t, sel.Missing)
		assert.Empty(t, sel.Unmatched)
	})

	t.Run("exclude wins", func(t *testing.T) {
		sel := Select(spec([]string{"*"}, "repositories.read_file", "code.t*"), siCatalog)
		assert.Len(t, sel.Tools, 8)
		assert.NotContains(t, sel.Tools, "repositories.read_file")
		assert.NotContains(t, sel.Tools, "code.trace")
	})

	t.Run("an excluded exact name is neither selected nor missing", func(t *testing.T) {
		sel := Select(spec([]string{"code.search", "code.trace"}, "code.trace"), siCatalog)
		assert.Equal(t, []string{"code.search"}, sel.Tools)
		assert.Empty(t, sel.Missing)
	})

	t.Run("a name the catalog lacks is missing", func(t *testing.T) {
		sel := Select(spec([]string{"code.search", "symbol_search"}), siCatalog)
		assert.Equal(t, []string{"code.search"}, sel.Tools)
		assert.Equal(t, []string{"symbol_search"}, sel.Missing)
	})

	t.Run("a pattern that selects nothing is unmatched", func(t *testing.T) {
		sel := Select(spec([]string{"cod.*", "refs.*"}), siCatalog)
		assert.Equal(t, []string{"cod.*"}, sel.Unmatched)
		assert.Equal(t, []string{"refs.list"}, sel.Tools)
	})

	t.Run("a pattern whose every match is excluded is unmatched too", func(t *testing.T) {
		sel := Select(spec([]string{"refs.*", "code.search"}, "refs.list"), siCatalog)
		assert.Equal(t, []string{"refs.*"}, sel.Unmatched)
	})

	t.Run("overlapping patterns select a tool once", func(t *testing.T) {
		sel := Select(spec([]string{"code.*", "*search", "code.search"}), siCatalog)
		assert.Len(t, sel.Tools, 5)
	})
}

// catalogsOf serves fixed catalogs; a server not in the map has no usable one.
func catalogsOf(byServer map[string][]string) Catalogs {
	return func(server string) ([]string, bool) {
		names, ok := byServer[server]
		return names, ok
	}
}

func TestResolve_PatternsExpandAgainstTheCatalog(t *testing.T) {
	ts := toolSet("si", "source-intelligence", "code.*", "repositories.*")
	ts.Spec.Exclude = []string{"repositories.read_file"}
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{ts},
		[]gatev1alpha1.MCPCapability{capability("research", "si")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "research")},
	)

	res := s.Resolve("agent", catalogsOf(map[string][]string{"source-intelligence": siCatalog}))
	assert.Len(t, res.Tools, 8)
	assert.Contains(t, res.Tools, "code.search")
	assert.Contains(t, res.Tools, "repositories.list_files")
	assert.NotContains(t, res.Tools, "repositories.read_file", "excluded")
	assert.NotContains(t, res.Tools, "refs.list", "matched by no pattern")

	_, ok := s.Authorize("agent", "repositories.read_file", catalogsOf(map[string][]string{"source-intelligence": siCatalog}))
	assert.False(t, ok, "an excluded tool is refused per call too")
}

// A pattern grants what the backend offers now — including a tool it added
// since the manifest was written.
func TestResolve_APatternPicksUpToolsTheBackendAddsLater(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{toolSet("si", "source-intelligence", "code.*")},
		[]gatev1alpha1.MCPCapability{capability("research", "si")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "research")},
	)
	later := append([]string{"code.callers"}, siCatalog...)

	assert.NotContains(t, s.Resolve("agent", catalogsOf(map[string][]string{"source-intelligence": siCatalog})).Tools, "code.callers")
	assert.Contains(t, s.Resolve("agent", catalogsOf(map[string][]string{"source-intelligence": later})).Tools, "code.callers")
}

// Without a usable catalog a pattern selects nothing, while an exact name still
// claims its name — which the gateway then does not expose, since the catalog
// does not have it either.
func TestResolve_WithoutACatalogPatternsSelectNothing(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{toolSet("si", "source-intelligence", "code.*", "refs.list")},
		[]gatev1alpha1.MCPCapability{capability("research", "si")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "research")},
	)
	assert.Equal(t, map[string]ToolRef{"refs.list": {Server: "source-intelligence", Tool: "refs.list"}},
		s.Resolve("agent", nil).Tools)
	assert.Equal(t, map[string]ToolRef{"refs.list": {Server: "source-intelligence", Tool: "refs.list"}},
		s.Resolve("agent", catalogsOf(map[string][]string{})).Tools)
}

// Fail closed holds for patterns: two backends whose patterns both match a name
// expose neither under it.
func TestResolve_PatternConflictsExposeNeither(t *testing.T) {
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("engram"), server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{
			toolSet("memory", "engram", "*"),
			toolSet("code", "source-intelligence", "code.*"),
		},
		[]gatev1alpha1.MCPCapability{capability("both", "memory", "code")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "both")},
	)
	catalogs := catalogsOf(map[string][]string{
		"engram":              {"remember", "code.search"},
		"source-intelligence": siCatalog,
	})

	res := s.Resolve("agent", catalogs)
	assert.NotContains(t, res.Tools, "code.search")
	assert.Equal(t, []ToolRef{{Server: "engram", Tool: "code.search"}, {Server: "source-intelligence", Tool: "code.search"}}, res.Conflicts["code.search"])
	assert.Contains(t, res.Tools, "remember")
	assert.Contains(t, res.Tools, "code.trace")
}

func TestResolve_ToolSetsBeingDeletedSelectNothing(t *testing.T) {
	ts := toolSet("si", "source-intelligence", "*")
	now := metav1.Now()
	ts.DeletionTimestamp = &now
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("source-intelligence")},
		[]gatev1alpha1.MCPToolSet{ts},
		[]gatev1alpha1.MCPCapability{capability("research", "si")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "research")},
	)
	assert.Empty(t, s.Resolve("agent", catalogsOf(map[string][]string{"source-intelligence": siCatalog})).Tools)
}
