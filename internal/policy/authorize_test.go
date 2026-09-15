package policy

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

// Authorize answers without resolving a subject's whole policy, and must still
// answer exactly what the resolution says. This checks that on policies built
// at random to hit every rule at once — exact names and patterns, exclusions,
// prefixes and aliases that collide with each other and with real tool names,
// references to things that do not exist.
func TestAuthorize_AgreesWithResolve(t *testing.T) {
	names := []string{"a", "b", "ab", "ba", "a.b", "p_a", "q_b", "x"}
	patterns := []string{"a*", "*b", "?", "*", "a?", "p_*"}
	prefixes := []string{"", "", "p_", "q_"}
	pick := func(r *rand.Rand, from []string, n int) []string {
		var out []string
		for range n {
			out = append(out, from[r.IntN(len(from))])
		}
		return sortedUnique(out)
	}

	for seed := range uint64(3000) {
		r := rand.New(rand.NewPCG(seed, 7))
		servers := []gatev1alpha1.MCPServer{server("s0"), server("s1"), server("s2")}
		catalogs := map[string][]string{}
		for _, srv := range []string{"s0", "s1", "s3"} { // s3 has a catalog and no MCPServer
			catalogs[srv] = pick(r, names, r.IntN(6))
		}

		var sets []gatev1alpha1.MCPToolSet
		for i := range 5 {
			ts := toolSet(fmt.Sprintf("set-%d", i), fmt.Sprintf("s%d", r.IntN(4)),
				append(pick(r, names, r.IntN(3)), pick(r, patterns, r.IntN(2))...)...)
			if len(ts.Spec.Tools) == 0 {
				ts.Spec.Tools = []string{"a"}
			}
			ts.Spec.Exclude = pick(r, append(slices.Clone(names), patterns...), r.IntN(2))
			ts.Spec.Prefix = prefixes[r.IntN(len(prefixes))]
			if r.IntN(2) == 0 {
				ts.Spec.Aliases = map[string]gatev1alpha1.ToolName{}
				used := map[string]bool{}
				for _, tool := range pick(r, names, r.IntN(3)) {
					alias := names[r.IntN(len(names))]
					if !used[alias] {
						used[alias] = true
						ts.Spec.Aliases[tool] = gatev1alpha1.ToolName(alias)
					}
				}
			}
			sets = append(sets, ts)
		}
		var caps []gatev1alpha1.MCPCapability
		for i := range 3 {
			var refs []string
			for _, j := range r.Perm(6)[:1+r.IntN(3)] {
				refs = append(refs, fmt.Sprintf("set-%d", j)) // set-5 does not exist
			}
			caps = append(caps, capability(fmt.Sprintf("cap-%d", i), refs...))
		}
		var grants []gatev1alpha1.MCPGrant
		for i := range 3 {
			grants = append(grants, grantFor(fmt.Sprintf("grant-%d", i), fmt.Sprintf("subject-%d", r.IntN(2)),
				pick(r, []string{"cap-0", "cap-1", "cap-2", "cap-9"}, 1+r.IntN(2))...))
		}

		s := NewSnapshot(servers, sets, caps, grants)
		lookup := Catalogs(func(srv string) ([]string, bool) {
			n, ok := catalogs[srv]
			return n, ok
		})

		for _, subject := range []string{"subject-0", "subject-1", "nobody"} {
			res := s.Resolve(subject, lookup)
			candidates := slices.Concat(names, patterns, []string{"p_ab", "q_a", "p_p_a", ""})
			for name := range res.Tools {
				candidates = append(candidates, name)
			}
			for name := range res.Conflicts {
				candidates = append(candidates, name)
			}
			for _, ts := range sets {
				for _, alias := range ts.Spec.Aliases {
					candidates = append(candidates, string(alias), ts.Spec.Prefix+string(alias))
				}
			}
			for _, name := range sortedUnique(candidates) {
				want, wantOK := res.Tools[name]
				got, gotOK := s.Authorize(subject, name, lookup)
				require.Equal(t, wantOK, gotOK, "seed %d, %s asks for %q", seed, subject, name)
				require.Equal(t, want, got, "seed %d, %s asks for %q", seed, subject, name)
			}
		}
	}
}

func TestAliasesAndPrefix(t *testing.T) {
	catalogs := Catalogs(func(srv string) ([]string, bool) {
		return map[string][]string{
			"engram": {"memory_search", "memory_list", "memory_remember"},
			"cortex": {"memory_search", "think"},
		}[srv], true
	})
	engram := toolSet("engram", "engram", "memory_*")
	engram.Spec.Exclude = []string{"memory_remember"}
	engram.Spec.Prefix = "engram_"
	engram.Spec.Aliases = map[string]gatev1alpha1.ToolName{"memory_search": "recall"}
	cortex := toolSet("cortex", "cortex", "*")
	s := NewSnapshot(
		[]gatev1alpha1.MCPServer{server("engram"), server("cortex")},
		[]gatev1alpha1.MCPToolSet{engram, cortex},
		[]gatev1alpha1.MCPCapability{capability("memory", "engram", "cortex")},
		[]gatev1alpha1.MCPGrant{grantFor("agent", "agent", "memory")},
	)

	res := s.Resolve("agent", catalogs)
	assert.Equal(t, map[string]ToolRef{
		"recall":             {Server: "engram", Tool: "memory_search"},
		"engram_memory_list": {Server: "engram", Tool: "memory_list"},
		"memory_search":      {Server: "cortex", Tool: "memory_search"},
		"think":              {Server: "cortex", Tool: "think"},
	}, res.Tools, "the two memory_search tools no longer collide")
	assert.Empty(t, res.Conflicts)

	ref, ok := s.Authorize("agent", "recall", catalogs)
	assert.True(t, ok)
	assert.Equal(t, ToolRef{Server: "engram", Tool: "memory_search"}, ref)
	_, ok = s.Authorize("agent", "engram_memory_search", catalogs)
	assert.False(t, ok, "an aliased tool is not also offered under its prefixed name")
	_, ok = s.Authorize("agent", "engram_memory_remember", catalogs)
	assert.False(t, ok, "exclusion uses the backend's name")
}

func TestSelect_ReportsRenamesAndUnusedAliases(t *testing.T) {
	spec := gatev1alpha1.MCPToolSetSpec{
		ServerRef: "engram",
		Tools:     []string{"memory_*", "forget"},
		Prefix:    "engram_",
		Aliases:   map[string]gatev1alpha1.ToolName{"memory_search": "recall", "memory_delete": "erase", "forget": "drop"},
	}
	sel := Select(spec, []string{"memory_search", "memory_list"})
	assert.Equal(t, []string{"engram_memory_list", "recall"}, sel.Tools)
	assert.Equal(t, map[string]string{"engram_memory_list": "memory_list", "recall": "memory_search"}, sel.BackendNames)
	assert.Equal(t, []string{"forget"}, sel.Missing)
	assert.Equal(t, []string{"memory_delete"}, sel.UnusedAliases, "an alias for a missing exact name is reported as missing, not unused")
}

// A tool set that would expose two of its own tools under one name exposes
// neither, and says so.
func TestSelect_ReportsItsOwnConflicts(t *testing.T) {
	spec := gatev1alpha1.MCPToolSetSpec{
		ServerRef: "engram",
		Tools:     []string{"*"},
		Prefix:    "e_",
		Aliases:   map[string]gatev1alpha1.ToolName{"search": "e_list", "forget": "drop"},
	}
	sel := Select(spec, []string{"search", "list", "forget"})
	assert.Equal(t, []string{"drop"}, sel.Tools)
	assert.Equal(t, []string{"e_list"}, sel.Conflicts)

	s := NewSnapshot([]gatev1alpha1.MCPServer{server("engram")},
		[]gatev1alpha1.MCPToolSet{{ObjectMeta: toolSet("engram", "engram").ObjectMeta, Spec: spec}},
		[]gatev1alpha1.MCPCapability{capability("c", "engram")},
		[]gatev1alpha1.MCPGrant{grantFor("g", "agent", "c")})
	catalogs := Catalogs(func(string) ([]string, bool) { return []string{"search", "list", "forget"}, true })
	res := s.Resolve("agent", catalogs)
	assert.Contains(t, res.Conflicts, "e_list")
	_, ok := s.Authorize("agent", "e_list", catalogs)
	assert.False(t, ok)
}
