// Package policy resolves what a subject may use: MCPGrant to MCPCapability to
// MCPToolSet to a tool on an MCPServer.
//
// It is pure. A Snapshot is built from the four kinds as listed at one moment,
// and resolution takes the backends' tool names as a further input; it has no
// client and no clock. The gateway resolves every call through it, and the
// controller reports through it what the gateway will do — one implementation,
// so the status a resource shows cannot disagree with the enforcement it
// describes.
//
// The catalogs are an input because tool sets select with patterns, and a
// pattern means nothing until it meets the names a backend offers. Exact names
// do not need them: a tool set naming a tool claims that name whether or not the
// backend currently has it. Whether a claimed tool is actually exposed is still
// the gateway's check against the catalog.
package policy

import (
	"slices"
	"sort"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
)

// ToolRef is the backend-qualified identity of a tool: the server it lives on
// and the name that server gives it. The gateway keeps this internally and
// exposes the bare name.
type ToolRef struct {
	Server string
	Tool   string
}

// String renders the ref as server/tool, the form used in logs and messages.
func (r ToolRef) String() string { return r.Server + "/" + r.Tool }

// Catalogs reports the tool names a server currently offers, and whether its
// catalog may be used at all. A nil Catalogs knows no catalog: patterns then
// select nothing, and exact names still claim what they name.
type Catalogs func(server string) (names []string, ok bool)

func (c Catalogs) names(server string) []string {
	if c == nil {
		return nil
	}
	names, ok := c(server)
	if !ok {
		return nil
	}
	return names
}

// Snapshot is the declared policy at one moment.
type Snapshot struct {
	servers      map[string]bool
	toolSets     map[string]mcpv1alpha1.MCPToolSetSpec
	capabilities map[string][]string
	// grants maps a subject to the capability names its grants name, in grant
	// name order so that resolution does not depend on list order.
	grants map[string][]grant
}

type grant struct {
	name         string
	capabilities []string
}

// NewSnapshot indexes the four kinds. Objects being deleted are left out: a
// grant whose deletion has been requested no longer grants anything, even while
// a finalizer holds it.
func NewSnapshot(
	servers []mcpv1alpha1.MCPServer,
	toolSets []mcpv1alpha1.MCPToolSet,
	capabilities []mcpv1alpha1.MCPCapability,
	grants []mcpv1alpha1.MCPGrant,
) *Snapshot {
	s := &Snapshot{
		servers:      make(map[string]bool, len(servers)),
		toolSets:     make(map[string]mcpv1alpha1.MCPToolSetSpec, len(toolSets)),
		capabilities: make(map[string][]string, len(capabilities)),
		grants:       map[string][]grant{},
	}
	for i := range servers {
		if servers[i].DeletionTimestamp.IsZero() {
			s.servers[servers[i].Name] = true
		}
	}
	for i := range toolSets {
		if toolSets[i].DeletionTimestamp.IsZero() {
			s.toolSets[toolSets[i].Name] = toolSets[i].Spec
		}
	}
	for i := range capabilities {
		if capabilities[i].DeletionTimestamp.IsZero() {
			s.capabilities[capabilities[i].Name] = capabilities[i].Spec.ToolSets
		}
	}
	for i := range grants {
		g := &grants[i]
		if !g.DeletionTimestamp.IsZero() {
			continue
		}
		s.grants[g.Spec.Subject] = append(s.grants[g.Spec.Subject], grant{name: g.Name, capabilities: g.Spec.Capabilities})
	}
	for subject := range s.grants {
		sort.Slice(s.grants[subject], func(i, j int) bool { return s.grants[subject][i].name < s.grants[subject][j].name })
	}
	return s
}

// Empty is the snapshot that grants nothing — what the gateway enforces before
// it has read any policy at all.
func Empty() *Snapshot {
	return NewSnapshot(nil, nil, nil, nil)
}

// Resolution is what a set of capabilities comes to.
type Resolution struct {
	// Tools maps each exposed name — after a tool set's prefix and aliases —
	// to the backend tool it stands for. A name that two different backend
	// tools claim is absent: see Conflicts.
	Tools map[string]ToolRef

	// Conflicts maps each name claimed by more than one backend tool to all of
	// its claimants, sorted. None of them is exposed. Picking one would make
	// what an agent calls depend on the order of a list, and failing closed is
	// the rule the whole model is built on.
	Conflicts map[string][]ToolRef

	// MissingCapabilities, MissingToolSets and MissingServers are the
	// references that did not resolve, sorted and without duplicates. They
	// contribute nothing to Tools.
	MissingCapabilities []string
	MissingToolSets     []string
	MissingServers      []string
}

// Subjects returns every subject some grant names, sorted.
func (s *Snapshot) Subjects() []string {
	subjects := make([]string, 0, len(s.grants))
	for subject := range s.grants {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	return subjects
}

// Resolve returns everything subject is granted, across all of its grants.
func (s *Snapshot) Resolve(subject string, catalogs Catalogs) Resolution {
	var capabilities []string
	for _, g := range s.grants[subject] {
		capabilities = append(capabilities, g.capabilities...)
	}
	return s.ResolveCapabilities(capabilities, catalogs)
}

// Authorize reports which backend tool subject reaches under name, if any.
// It is the check the gateway makes on every tools/call, and it answers exactly
// what Resolve(subject).Tools[name] would.
//
// It gets there without resolving the subject's whole policy. Every tool set
// the subject's grants reach is asked only whether it exposes this one name:
// Exposed inverted gives at most a couple of backend tools it could stand for,
// and each is checked against the set's selection. A catalog is only read for a
// tool a pattern matches. Two different backend tools behind the name are a
// conflict, and a conflict exposes neither.
func (s *Snapshot) Authorize(subject, name string, catalogs Catalogs) (ToolRef, bool) {
	var found ToolRef
	have := false
	for _, g := range s.grants[subject] {
		for _, capability := range g.capabilities {
			for _, set := range s.capabilities[capability] {
				spec, ok := s.toolSets[set]
				if !ok || !s.servers[spec.ServerRef] {
					continue
				}
				for _, tool := range backendNames(spec, name) {
					available := func() []string { return catalogs.names(spec.ServerRef) }
					if !claimsTool(spec, tool, available) {
						continue
					}
					ref := ToolRef{Server: spec.ServerRef, Tool: tool}
					switch {
					case !have:
						found, have = ref, true
					case ref != found:
						return ToolRef{}, false
					}
				}
			}
		}
	}
	return found, have
}

// ResolveCapabilities returns what the named capabilities come to together.
func (s *Snapshot) ResolveCapabilities(names []string, catalogs Catalogs) Resolution {
	var toolSets []string
	var missingCapabilities []string
	for _, name := range names {
		sets, ok := s.capabilities[name]
		if !ok {
			missingCapabilities = append(missingCapabilities, name)
			continue
		}
		toolSets = append(toolSets, sets...)
	}
	res := s.ResolveToolSets(toolSets, catalogs)
	res.MissingCapabilities = sortedUnique(missingCapabilities)
	return res
}

// ResolveToolSets returns what the named tool sets come to together.
func (s *Snapshot) ResolveToolSets(names []string, catalogs Catalogs) Resolution {
	claimants := map[string][]ToolRef{}
	var missingToolSets, missingServers []string
	for _, name := range names {
		spec, ok := s.toolSets[name]
		if !ok {
			missingToolSets = append(missingToolSets, name)
			continue
		}
		if !s.servers[spec.ServerRef] {
			missingServers = append(missingServers, spec.ServerRef)
			continue
		}
		for _, c := range claims(spec, catalogs.names(spec.ServerRef)) {
			ref := ToolRef{Server: spec.ServerRef, Tool: c.tool}
			// The same backend tool reached through two tool sets under the
			// same name is one tool, not a conflict.
			if !slices.Contains(claimants[c.exposed], ref) {
				claimants[c.exposed] = append(claimants[c.exposed], ref)
			}
		}
	}

	res := Resolution{
		Tools:           make(map[string]ToolRef, len(claimants)),
		Conflicts:       map[string][]ToolRef{},
		MissingToolSets: sortedUnique(missingToolSets),
		MissingServers:  sortedUnique(missingServers),
	}
	for name, refs := range claimants {
		if len(refs) == 1 {
			res.Tools[name] = refs[0]
			continue
		}
		sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
		res.Conflicts[name] = refs
	}
	return res
}

// ConflictNames returns the conflicting names of a resolution, sorted.
func (r Resolution) ConflictNames() []string {
	names := make([]string, 0, len(r.Conflicts))
	for name := range r.Conflicts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	sort.Strings(out)
	return slices.Compact(out)
}
