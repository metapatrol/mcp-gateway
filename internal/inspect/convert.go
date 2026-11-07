package inspect

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcpv1alpha1 "forgejo.metapatrol.local/platform/mcp-gateway/api/v1alpha1"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/discovery"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
	"forgejo.metapatrol.local/platform/mcp-gateway/internal/policy"
)

// Spec and status go from the CRD types to the API types through JSON: the API
// mirrors the manifests field for field, and the JSON names are the contract
// both sides share. TestAPIMirrorsTheCRDs keeps the two from drifting apart.
func viaJSON(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func meta(obj metav1.Object) api.Meta {
	m := api.Meta{
		Name:              obj.GetName(),
		Generation:        obj.GetGeneration(),
		CreationTimestamp: obj.GetCreationTimestamp().UTC(),
	}
	if labels := obj.GetLabels(); len(labels) > 0 {
		m.Labels = &labels
	}
	return m
}

func conditions(in []metav1.Condition) []api.Condition {
	out := make([]api.Condition, 0, len(in))
	for _, c := range in {
		ac := api.Condition{
			Type:               api.ConditionType(c.Type),
			Status:             api.ConditionStatus(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.UTC(),
		}
		if c.ObservedGeneration != 0 {
			g := c.ObservedGeneration
			ac.ObservedGeneration = &g
		}
		out = append(out, ac)
	}
	return out
}

func serverSpec(s *mcpv1alpha1.MCPServer) (api.ServerSpec, error) {
	var out api.ServerSpec
	if err := viaJSON(s.Spec, &out); err != nil {
		return out, fmt.Errorf("MCPServer %s: spec: %w", s.Name, err)
	}
	if out.DiscoveryInterval == "" {
		out.DiscoveryInterval = mcpv1alpha1.DefaultDiscoveryInterval.String()
	}
	if out.Auth.Type == "" {
		out.Auth.Type = api.BackendAuthType(s.AuthType())
	}
	out.Transport = api.ServerSpecTransport(s.TransportType())
	return out, nil
}

func serverStatus(s *mcpv1alpha1.MCPServer) (api.ServerStatus, error) {
	var out api.ServerStatus
	if err := viaJSON(s.Status, &out); err != nil {
		return out, fmt.Errorf("MCPServer %s: status: %w", s.Name, err)
	}
	out.Conditions = conditions(s.Status.Conditions)
	return out, nil
}

func toolSetSummary(ts *mcpv1alpha1.MCPToolSet) (api.ToolSetSummary, error) {
	out := api.ToolSetSummary{Meta: meta(ts)}
	if err := viaJSON(ts.Spec, &out.Spec); err != nil {
		return out, fmt.Errorf("MCPToolSet %s: spec: %w", ts.Name, err)
	}
	if err := viaJSON(ts.Status, &out.Status); err != nil {
		return out, fmt.Errorf("MCPToolSet %s: status: %w", ts.Name, err)
	}
	out.Status.Conditions = conditions(ts.Status.Conditions)
	return out, nil
}

func capabilitySummary(c *mcpv1alpha1.MCPCapability) api.CapabilitySummary {
	out := api.CapabilitySummary{
		Meta: meta(c),
		Spec: api.CapabilitySpec{ToolSets: nonNil(c.Spec.ToolSets)},
		Status: api.CapabilityStatus{
			Conditions: conditions(c.Status.Conditions),
		},
	}
	if c.Spec.Description != "" {
		out.Spec.Description = &c.Spec.Description
	}
	if c.Status.ObservedGeneration != 0 {
		out.Status.ObservedGeneration = &c.Status.ObservedGeneration
	}
	return out
}

func grantSummary(g *mcpv1alpha1.MCPGrant) api.GrantSummary {
	out := api.GrantSummary{
		Meta: meta(g),
		Spec: api.GrantSpec{Subject: g.Spec.Subject, Capabilities: nonNil(g.Spec.Capabilities)},
		Status: api.GrantStatus{
			Conditions: conditions(g.Status.Conditions),
		},
	}
	if g.Status.ObservedGeneration != 0 {
		out.Status.ObservedGeneration = &g.Status.ObservedGeneration
	}
	return out
}

// toolDefinition is a backend's tool as the API shows it. It goes through JSON
// because the SDK holds the schemas as whatever the backend sent; the fields
// the API does not carry (_meta, icons) fall away.
func toolDefinition(t *mcp.Tool) api.ToolDefinition {
	var out api.ToolDefinition
	if t == nil {
		return out
	}
	if err := viaJSON(t, &out); err != nil || out.Name == "" {
		// A schema that does not round-trip still has a name worth showing.
		out = api.ToolDefinition{Name: t.Name}
		if t.Description != "" {
			out.Description = &t.Description
		}
	}
	if out.InputSchema == nil {
		out.InputSchema = map[string]any{}
	}
	return out
}

func catalog(c *discovery.Catalog) *api.Catalog {
	out := &api.Catalog{Revision: c.Revision, Instructions: c.Instructions, Tools: []api.ToolDefinition{}}
	for _, name := range c.Names() {
		t, _ := c.Tool(name)
		out.Tools = append(out.Tools, toolDefinition(t))
	}
	return out
}

func toolRef(r policy.ToolRef) api.ToolRef {
	return api.ToolRef{Server: r.Server, Tool: r.Tool}
}

func explanation(ex policy.Explanation, definition func(tool string) api.ToolDefinition) api.SelectionExplanation {
	out := api.SelectionExplanation{
		Tools:         []api.CatalogToolSelection{},
		Entries:       []api.SelectionEntry{},
		Exclusions:    []api.ExclusionEntry{},
		Aliases:       []api.AliasEntry{},
		Missing:       nonNil(ex.Missing),
		Unmatched:     nonNil(ex.Unmatched),
		UnusedAliases: nonNil(ex.UnusedAliases),
		Conflicts:     nonNil(ex.Conflicts),
	}
	for _, t := range ex.Tools {
		ct := api.CatalogToolSelection{
			Tool:       definition(t.Tool),
			State:      api.CatalogToolSelectionState(t.State),
			SelectedBy: nonNil(t.SelectedBy),
			ExcludedBy: nonNil(t.ExcludedBy),
		}
		if t.Exposed != "" {
			ct.ExposedAs = &t.Exposed
		}
		if t.RenamedBy != "" {
			r := api.CatalogToolSelectionRenamedBy(t.RenamedBy)
			ct.RenamedBy = &r
		}
		out.Tools = append(out.Tools, ct)
	}
	for _, e := range ex.Entries {
		out.Entries = append(out.Entries, api.SelectionEntry{
			Entry:   e.Entry,
			Kind:    entryKind[e.Pattern],
			Outcome: api.SelectionEntryOutcome(e.Outcome),
			Selects: nonNil(e.Selects),
		})
	}
	for _, x := range ex.Exclusions {
		kind := api.ExclusionEntryKindName
		if x.Pattern {
			kind = api.ExclusionEntryKindPattern
		}
		out.Exclusions = append(out.Exclusions, api.ExclusionEntry{Entry: x.Entry, Kind: kind, Removes: nonNil(x.Removes)})
	}
	for _, a := range ex.Aliases {
		out.Aliases = append(out.Aliases, api.AliasEntry{Tool: a.Tool, Alias: a.Alias, Used: a.Used})
	}
	return out
}

var entryKind = map[bool]api.SelectionEntryKind{false: api.SelectionEntryKindName, true: api.SelectionEntryKindPattern}
