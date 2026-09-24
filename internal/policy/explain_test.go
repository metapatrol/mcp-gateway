package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
)

func explained(ex Explanation, tool string) ToolExplanation {
	for _, t := range ex.Tools {
		if t.Tool == tool {
			return t
		}
	}
	return ToolExplanation{}
}

func TestExplain(t *testing.T) {
	t.Run("every catalog tool, with the entries that decided it", func(t *testing.T) {
		ex := Explain(spec([]string{"code.*", "repositories.*", "refs.list"}, "repositories.read_file"), siCatalog)

		require.Len(t, ex.Tools, len(siCatalog))
		assert.Equal(t, ToolExplanation{
			Tool: "code.search", State: ToolSelected, Exposed: "code.search", SelectedBy: []string{"code.*"},
		}, explained(ex, "code.search"))
		assert.Equal(t, ToolExplanation{
			Tool: "repositories.read_file", State: ToolExcluded,
			SelectedBy: []string{"repositories.*"}, ExcludedBy: []string{"repositories.read_file"},
		}, explained(ex, "repositories.read_file"))
		assert.Equal(t, ToolSelected, explained(ex, "refs.list").State)

		assert.Equal(t, []EntryExplanation{
			{Entry: "code.*", Pattern: true, Outcome: EntryOK, Selects: []string{"code.architecture", "code.inventory", "code.search", "code.symbol", "code.trace"}},
			{Entry: "repositories.*", Pattern: true, Outcome: EntryOK, Selects: []string{"repositories.get", "repositories.list", "repositories.list_files"}},
			{Entry: "refs.list", Outcome: EntryOK, Selects: []string{"refs.list"}},
		}, ex.Entries)
		assert.Equal(t, []ExclusionExplanation{
			{Entry: "repositories.read_file", Removes: []string{"repositories.read_file"}},
		}, ex.Exclusions)
	})

	t.Run("a tool no entry matches is unselected", func(t *testing.T) {
		ex := Explain(spec([]string{"code.search"}), siCatalog)
		assert.Equal(t, ToolExplanation{Tool: "refs.list", State: ToolUnselected}, explained(ex, "refs.list"))
	})

	t.Run("an exclusion of a tool nothing selects removes nothing", func(t *testing.T) {
		ex := Explain(spec([]string{"code.search"}, "refs.*"), siCatalog)
		assert.Equal(t, ToolUnselected, explained(ex, "refs.list").State)
		assert.Equal(t, []string{"refs.*"}, explained(ex, "refs.list").ExcludedBy)
		assert.Empty(t, ex.Exclusions[0].Removes)
	})

	t.Run("entry outcomes", func(t *testing.T) {
		ex := Explain(spec([]string{"code.search", "code.gone", "repositories.read_file", "nothing.*", "refs.*"}, "repositories.read_file", "refs.*"), siCatalog)
		outcomes := map[string]EntryOutcome{}
		for _, e := range ex.Entries {
			outcomes[e.Entry] = e.Outcome
		}
		assert.Equal(t, map[string]EntryOutcome{
			"code.search":            EntryOK,
			"code.gone":              EntryMissing,
			"repositories.read_file": EntryExcluded,
			"nothing.*":              EntryUnmatched,
			"refs.*":                 EntryUnmatched, // every match excluded
		}, outcomes)
		assert.Equal(t, []string{"code.gone"}, ex.Missing)
		assert.Equal(t, []string{"nothing.*", "refs.*"}, ex.Unmatched)
	})

	t.Run("prefix and aliases", func(t *testing.T) {
		s := mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "memory",
			Tools:     []string{"memory_*"},
			Prefix:    "engram_",
			Aliases:   map[string]mcpv1alpha1.ToolName{"memory_search": "recall", "memory_export": "export"},
		}
		ex := Explain(s, []string{"memory_search", "memory_store"})
		assert.Equal(t, ToolExplanation{
			Tool: "memory_search", State: ToolSelected, Exposed: "recall", RenamedBy: "alias", SelectedBy: []string{"memory_*"},
		}, explained(ex, "memory_search"))
		assert.Equal(t, "engram_memory_store", explained(ex, "memory_store").Exposed)
		assert.Equal(t, "prefix", explained(ex, "memory_store").RenamedBy)
		assert.Equal(t, []AliasExplanation{
			{Tool: "memory_export", Alias: "export", Used: false},
			{Tool: "memory_search", Alias: "recall", Used: true},
		}, ex.Aliases)
	})

	t.Run("a conflict inside the set", func(t *testing.T) {
		s := mcpv1alpha1.MCPToolSetSpec{
			ServerRef: "memory",
			Tools:     []string{"*"},
			Aliases:   map[string]mcpv1alpha1.ToolName{"memory_search": "memory_store"},
		}
		ex := Explain(s, []string{"memory_search", "memory_store"})
		assert.Equal(t, ToolConflict, explained(ex, "memory_search").State)
		assert.Equal(t, ToolConflict, explained(ex, "memory_store").State)
		assert.Equal(t, []string{"memory_search", "memory_store"}, ex.Entries[0].Selects)
		assert.Equal(t, []string{"memory_store"}, ex.Conflicts)
	})

	t.Run("agrees with Select", func(t *testing.T) {
		for _, s := range []mcpv1alpha1.MCPToolSetSpec{
			spec([]string{"code.*", "refs.list"}),
			spec([]string{"*"}, "code.*"),
			spec([]string{"code.search", "code.gone", "nothing.*"}),
		} {
			ex := Explain(s, siCatalog)
			assert.Equal(t, Select(s, siCatalog), ex.Selection)
			var exposed []string
			for _, tool := range ex.Tools {
				if tool.State == ToolSelected {
					exposed = append(exposed, tool.Exposed)
				}
			}
			assert.ElementsMatch(t, ex.Selection.Tools, exposed)
		}
	})

	t.Run("no catalog", func(t *testing.T) {
		ex := Explain(spec([]string{"code.*", "refs.list"}), nil)
		assert.Empty(t, ex.Tools)
		assert.Equal(t, EntryUnmatched, ex.Entries[0].Outcome)
		assert.Equal(t, EntryMissing, ex.Entries[1].Outcome)
	})
}

func TestClaims(t *testing.T) {
	s := mcpv1alpha1.MCPToolSetSpec{ServerRef: "si", Tools: []string{"code.*", "gone"}, Exclude: []string{"code.trace"}, Prefix: "si_"}
	assert.Equal(t, []Claim{
		{Tool: "code.architecture", Exposed: "si_code.architecture"},
		{Tool: "code.inventory", Exposed: "si_code.inventory"},
		{Tool: "code.search", Exposed: "si_code.search"},
		{Tool: "code.symbol", Exposed: "si_code.symbol"},
		{Tool: "gone", Exposed: "si_gone"}, // exact names are claimed whether or not the catalog has them
	}, Claims(s, siCatalog))
}
