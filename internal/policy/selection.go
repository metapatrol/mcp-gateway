package policy

import (
	"slices"
	"sort"
	"strings"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

// A tool set selects tools with names and glob patterns.
//
// Globs rather than regular expressions: tool names are dotted and underscored
// identifiers, and "code.*" says what it means where `^code\..*$` needs to be
// read twice — in YAML, with its own escaping on top. An exact name is simply a
// pattern without a wildcard, so every tool set written before patterns existed
// still means what it meant.

// IsPattern reports whether s selects by wildcard rather than naming one tool.
func IsPattern(s string) bool { return strings.ContainsAny(s, "*?") }

// Match reports whether name matches the glob pattern. "*" matches any run of
// characters, the empty run and dots included; "?" matches exactly one
// character. Everything else matches itself, case-sensitively as tool names
// are. There is no escaping: neither wildcard belongs in a tool name.
func Match(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	pi, ni := 0, 0
	star, mark := -1, 0
	for ni < len(n) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == n[ni]):
			pi++
			ni++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ni
			pi++
		case star >= 0:
			// Let the last star swallow one more character and retry.
			pi = star + 1
			mark++
			ni = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// Selection is what one tool set selects from one catalog.
type Selection struct {
	// Tools are the selected tools the catalog contains, sorted.
	Tools []string

	// Missing are the exact names the tool set lists, does not exclude, and
	// the catalog lacks, sorted.
	Missing []string

	// Unmatched are the patterns that select no tool at all, sorted — after
	// exclusion, so a pattern whose every match is excluded counts. A
	// wildcard that selects nothing is almost always a typo.
	Unmatched []string
}

// Select resolves a tool set against the tool names a catalog holds.
func Select(spec gatev1alpha1.MCPToolSetSpec, available []string) Selection {
	var sel Selection
	selected := map[string]bool{}
	for _, entry := range spec.Tools {
		if !IsPattern(entry) {
			switch {
			case excluded(spec, entry):
			case slices.Contains(available, entry):
				selected[entry] = true
			default:
				sel.Missing = append(sel.Missing, entry)
			}
			continue
		}
		matched := false
		for _, name := range available {
			if Match(entry, name) && !excluded(spec, name) {
				selected[name] = true
				matched = true
			}
		}
		if !matched {
			sel.Unmatched = append(sel.Unmatched, entry)
		}
	}
	for name := range selected {
		sel.Tools = append(sel.Tools, name)
	}
	sort.Strings(sel.Tools)
	sel.Missing = sortedUnique(sel.Missing)
	sel.Unmatched = sortedUnique(sel.Unmatched)
	return sel
}

// claims returns the names a tool set lays claim to: every exact name it lists,
// whether or not the catalog has it, and every catalog tool a pattern matches —
// in both cases minus what it excludes.
//
// Exact names are claimed regardless of the catalog so that a conflict between
// two named tools stays a property of the manifests, as it was before patterns:
// it does not appear the moment the second backend happens to come up. Patterns
// can only be expanded against what a backend offers, so a conflict they cause
// appears when the backend offers the tool.
func claims(spec gatev1alpha1.MCPToolSetSpec, available []string) []string {
	var out []string
	for _, entry := range spec.Tools {
		if !IsPattern(entry) {
			if !excluded(spec, entry) {
				out = append(out, entry)
			}
			continue
		}
		for _, name := range available {
			if Match(entry, name) && !excluded(spec, name) {
				out = append(out, name)
			}
		}
	}
	return sortedUnique(out)
}

func excluded(spec gatev1alpha1.MCPToolSetSpec, name string) bool {
	for _, pattern := range spec.Exclude {
		if Match(pattern, name) {
			return true
		}
	}
	return false
}
