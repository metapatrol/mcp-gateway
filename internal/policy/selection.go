package policy

import (
	"slices"
	"sort"
	"strings"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
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

// Exposed is the name agents see a backend tool under, through a tool set: its
// alias if it has one, the prefix and the backend's name otherwise. It is the
// one place a tool set renames anything; resolution and authorisation both go
// through it.
func Exposed(spec mcpv1alpha1.MCPToolSetSpec, tool string) string {
	if alias, ok := spec.Aliases[tool]; ok {
		return string(alias)
	}
	return spec.Prefix + tool
}

// backendNames returns the backend tools a tool set would expose under name,
// if it selected them: Exposed inverted. There are at most a few — an alias,
// and the name without the prefix unless that tool has an alias of its own.
func backendNames(spec mcpv1alpha1.MCPToolSetSpec, name string) []string {
	var out []string
	for tool, alias := range spec.Aliases {
		if string(alias) == name {
			out = append(out, tool)
		}
	}
	if tool, ok := strings.CutPrefix(name, spec.Prefix); ok && tool != "" {
		if _, aliased := spec.Aliases[tool]; !aliased {
			out = append(out, tool)
		}
	}
	return out
}

// Selection is what one tool set selects from one catalog.
type Selection struct {
	// Tools are the selected tools the catalog contains, under the names agents
	// see them, sorted.
	Tools []string

	// BackendNames maps each of Tools offered under a name other than the
	// backend's to the backend's name. Nil when nothing is renamed.
	BackendNames map[string]string

	// Missing are the exact names the tool set lists, does not exclude, and
	// the catalog lacks, sorted.
	Missing []string

	// Unmatched are the patterns that select no tool at all, sorted — after
	// exclusion, so a pattern whose every match is excluded counts. A
	// wildcard that selects nothing is almost always a typo.
	Unmatched []string

	// UnusedAliases are the alias keys that name no tool the set selects,
	// sorted — neither one the catalog holds nor an exact name it lacks.
	UnusedAliases []string

	// Conflicts are the names under which the set would expose more than one of
	// the tools it selects, sorted. None of them is in Tools: a conflict
	// exposes neither, here as in a capability.
	Conflicts []string
}

// Select resolves a tool set against the tool names a catalog holds.
func Select(spec mcpv1alpha1.MCPToolSetSpec, available []string) Selection {
	var sel Selection
	selected := map[string]bool{}
	named := map[string]bool{}
	for _, entry := range spec.Tools {
		if !IsPattern(entry) {
			switch {
			case excluded(spec, entry):
			case slices.Contains(available, entry):
				selected[entry] = true
			default:
				named[entry] = true
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
	byName := map[string][]string{}
	for tool := range selected {
		exposed := Exposed(spec, tool)
		byName[exposed] = append(byName[exposed], tool)
	}
	for exposed, tools := range byName {
		if len(tools) > 1 {
			sel.Conflicts = append(sel.Conflicts, exposed)
			continue
		}
		sel.Tools = append(sel.Tools, exposed)
		if exposed != tools[0] {
			if sel.BackendNames == nil {
				sel.BackendNames = map[string]string{}
			}
			sel.BackendNames[exposed] = tools[0]
		}
	}
	sort.Strings(sel.Conflicts)
	for tool := range spec.Aliases {
		if !selected[tool] && !named[tool] {
			sel.UnusedAliases = append(sel.UnusedAliases, tool)
		}
	}
	sort.Strings(sel.Tools)
	sel.Missing = sortedUnique(sel.Missing)
	sel.Unmatched = sortedUnique(sel.Unmatched)
	sel.UnusedAliases = sortedUnique(sel.UnusedAliases)
	return sel
}

// claim is one tool a tool set lays claim to: the backend's name, and the name
// agents see it under.
type claim struct {
	tool    string
	exposed string
}

// claims returns the tools a tool set lays claim to: every exact name it lists,
// whether or not the catalog has it, and every catalog tool a pattern matches —
// in both cases minus what it excludes.
//
// Exact names are claimed regardless of the catalog so that a conflict between
// two named tools stays a property of the manifests, as it was before patterns:
// it does not appear the moment the second backend happens to come up. Patterns
// can only be expanded against what a backend offers, so a conflict they cause
// appears when the backend offers the tool.
func claims(spec mcpv1alpha1.MCPToolSetSpec, available []string) []claim {
	var tools []string
	for _, entry := range spec.Tools {
		if !IsPattern(entry) {
			if !excluded(spec, entry) {
				tools = append(tools, entry)
			}
			continue
		}
		for _, name := range available {
			if Match(entry, name) && !excluded(spec, name) {
				tools = append(tools, name)
			}
		}
	}
	tools = sortedUnique(tools)
	out := make([]claim, len(tools))
	for i, tool := range tools {
		out[i] = claim{tool: tool, exposed: Exposed(spec, tool)}
	}
	return out
}

// claimsTool reports whether a tool set lays claim to one backend tool, by the
// same rules as claims. available is only consulted for a tool a pattern
// matches, which is the one case the catalog decides.
func claimsTool(spec mcpv1alpha1.MCPToolSetSpec, tool string, available func() []string) bool {
	if excluded(spec, tool) {
		return false
	}
	patternMatches := false
	for _, entry := range spec.Tools {
		if !IsPattern(entry) {
			if entry == tool {
				return true
			}
			continue
		}
		if Match(entry, tool) {
			patternMatches = true
		}
	}
	return patternMatches && slices.Contains(available(), tool)
}

func excluded(spec mcpv1alpha1.MCPToolSetSpec, name string) bool {
	for _, pattern := range spec.Exclude {
		if Match(pattern, name) {
			return true
		}
	}
	return false
}
