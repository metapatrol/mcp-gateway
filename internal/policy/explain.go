package policy

import (
	"slices"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
)

// ToolState is what a tool set does with one tool of its server's catalog.
type ToolState string

const (
	// ToolSelected: exposed, under Exposed.
	ToolSelected ToolState = "selected"
	// ToolExcluded: an entry of tools matches it, and one of exclude removes it.
	ToolExcluded ToolState = "excluded"
	// ToolUnselected: no entry of tools matches it.
	ToolUnselected ToolState = "unselected"
	// ToolConflict: selected, but the set would expose another tool under the
	// same name, so neither is exposed.
	ToolConflict ToolState = "conflict"
)

// EntryOutcome is what one entry of a tool set's tools comes to.
type EntryOutcome string

const (
	// EntryOK: the entry selects at least one tool.
	EntryOK EntryOutcome = "ok"
	// EntryMissing: an exact name the catalog lacks.
	EntryMissing EntryOutcome = "missing"
	// EntryUnmatched: a pattern that selects nothing, after exclusion.
	EntryUnmatched EntryOutcome = "unmatched"
	// EntryExcluded: an exact name that exclude removes again.
	EntryExcluded EntryOutcome = "excluded"
)

// Explanation is a Selection with its reasons kept: for every tool of the
// catalog, which entries decided what happens to it, and for every entry, what
// it does. It answers the question a Selection cannot — why a tool is or is not
// exposed — and is meant for people; enforcement goes through Select and the
// Snapshot, never through this.
type Explanation struct {
	Selection

	// Tools covers every tool of the catalog, sorted by the backend's name.
	Tools []ToolExplanation
	// Entries are spec.tools, in order.
	Entries []EntryExplanation
	// Exclusions are spec.exclude, in order.
	Exclusions []ExclusionExplanation
	// Aliases are spec.aliases, sorted by key.
	Aliases []AliasExplanation
}

// ToolExplanation is what a tool set does with one catalog tool, and why.
type ToolExplanation struct {
	Tool  string
	State ToolState
	// Exposed is the name agents see; set when State is ToolSelected or
	// ToolConflict.
	Exposed string
	// RenamedBy is "alias" or "prefix" when Exposed differs from Tool.
	RenamedBy string
	// SelectedBy are the entries of spec.tools that match the tool.
	SelectedBy []string
	// ExcludedBy are the entries of spec.exclude that match the tool.
	ExcludedBy []string
}

// EntryExplanation is what one entry of spec.tools comes to.
type EntryExplanation struct {
	Entry   string
	Pattern bool
	Outcome EntryOutcome
	// Selects are the backend names the entry contributes, after exclusion,
	// sorted. A tool in a conflict counts: the entry selects it, the conflict
	// is what keeps it from being exposed.
	Selects []string
}

// ExclusionExplanation is what one entry of spec.exclude removes.
type ExclusionExplanation struct {
	Entry   string
	Pattern bool
	// Removes are the backend names spec.tools would have selected and this
	// entry takes out, sorted.
	Removes []string
}

// AliasExplanation is one entry of spec.aliases.
type AliasExplanation struct {
	Tool  string
	Alias string
	// Used is whether the set selects the tool the key names.
	Used bool
}

// Explain resolves a tool set against the tool names a catalog holds, as
// Select does, and keeps the reasons. Its Selection is Select's, so the two
// never disagree.
func Explain(spec mcpv1alpha1.MCPToolSetSpec, available []string) Explanation {
	ex := Explanation{Selection: Select(spec, available)}
	conflicts := map[string]bool{}
	for _, name := range ex.Conflicts {
		conflicts[name] = true
	}

	tools := slices.Clone(available)
	slices.Sort(tools)
	tools = slices.Compact(tools)
	for _, tool := range tools {
		t := ToolExplanation{Tool: tool, State: ToolUnselected}
		for _, entry := range spec.Tools {
			if Match(entry, tool) {
				t.SelectedBy = append(t.SelectedBy, entry)
			}
		}
		for _, entry := range spec.Exclude {
			if Match(entry, tool) {
				t.ExcludedBy = append(t.ExcludedBy, entry)
			}
		}
		switch {
		case len(t.SelectedBy) == 0:
		case len(t.ExcludedBy) > 0:
			t.State = ToolExcluded
		default:
			t.Exposed = Exposed(spec, tool)
			t.State = ToolSelected
			if conflicts[t.Exposed] {
				t.State = ToolConflict
			}
			if _, ok := spec.Aliases[tool]; ok {
				t.RenamedBy = "alias"
			} else if spec.Prefix != "" {
				t.RenamedBy = "prefix"
			}
		}
		ex.Tools = append(ex.Tools, t)
	}

	for _, entry := range spec.Tools {
		e := EntryExplanation{Entry: entry, Pattern: IsPattern(entry), Outcome: EntryOK}
		for _, t := range ex.Tools {
			if (t.State == ToolSelected || t.State == ToolConflict) && slices.Contains(t.SelectedBy, entry) {
				e.Selects = append(e.Selects, t.Tool)
			}
		}
		switch {
		case e.Pattern && len(e.Selects) == 0:
			e.Outcome = EntryUnmatched
		case e.Pattern:
		case excluded(spec, entry):
			e.Outcome = EntryExcluded
		case !slices.Contains(tools, entry):
			e.Outcome = EntryMissing
		}
		ex.Entries = append(ex.Entries, e)
	}

	for _, entry := range spec.Exclude {
		x := ExclusionExplanation{Entry: entry, Pattern: IsPattern(entry)}
		for _, t := range ex.Tools {
			if len(t.SelectedBy) > 0 && slices.Contains(t.ExcludedBy, entry) {
				x.Removes = append(x.Removes, t.Tool)
			}
		}
		ex.Exclusions = append(ex.Exclusions, x)
	}

	keys := make([]string, 0, len(spec.Aliases))
	for tool := range spec.Aliases {
		keys = append(keys, tool)
	}
	slices.Sort(keys)
	for _, tool := range keys {
		ex.Aliases = append(ex.Aliases, AliasExplanation{
			Tool:  tool,
			Alias: string(spec.Aliases[tool]),
			Used:  !slices.Contains(ex.UnusedAliases, tool),
		})
	}
	return ex
}

// Claim is one tool a tool set lays claim to: the backend's name, and the name
// agents see it under.
type Claim struct {
	Tool    string
	Exposed string
}

// Claims returns the tools a tool set lays claim to, by the rules resolution
// uses: every exact name it lists whether or not the catalog has it, and every
// catalog tool a pattern matches, minus what it excludes. It is what lets a
// caller tell which tool sets a resolved tool came through.
func Claims(spec mcpv1alpha1.MCPToolSetSpec, available []string) []Claim {
	in := claims(spec, available)
	out := make([]Claim, len(in))
	for i, c := range in {
		out[i] = Claim{Tool: c.tool, Exposed: c.exposed}
	}
	return out
}
