package inspect

import (
	"strings"

	"forgejo.metapatrol.local/platform/mcp-gateway/internal/inspect/api"
)

// What each list searches and sorts by, with the column keys of the spec's
// sort parameter and of the console's tables.

func conditionOf(conditions []api.Condition, typ string) sortValue {
	for _, c := range conditions {
		if string(c.Type) == typ {
			return conditionRank(string(c.Status))
		}
	}
	return conditionRank("")
}

var serverListing = listing[api.ServerSummary]{
	name: func(s api.ServerSummary) string { return s.Meta.Name },
	texts: func(s api.ServerSummary) []string {
		return []string{s.Meta.Name, s.Spec.Endpoint, string(s.Spec.Auth.Type)}
	},
	columns: map[string]func(api.ServerSummary) sortValue{
		"name":     func(s api.ServerSummary) sortValue { return textValue(s.Meta.Name) },
		"endpoint": func(s api.ServerSummary) sortValue { return textValue(s.Spec.Endpoint) },
		"auth":     func(s api.ServerSummary) sortValue { return textValue(string(s.Spec.Auth.Type)) },
		"tools":    func(s api.ServerSummary) sortValue { return optNum(s.Discovery.ToolCount) },
		"discovery": func(s api.ServerSummary) sortValue {
			if s.Discovery.LastSuccess == nil {
				return sortValue{}
			}
			return numValue(float64(s.Discovery.LastSuccess.UnixMilli()))
		},
		"ready": func(s api.ServerSummary) sortValue { return conditionOf(s.Status.Conditions, "Ready") },
	},
}

var toolSetListing = listing[api.ToolSetSummary]{
	name: func(t api.ToolSetSummary) string { return t.Meta.Name },
	texts: func(t api.ToolSetSummary) []string {
		texts := []string{t.Meta.Name, t.Spec.ServerRef, ptrOr(t.Spec.Prefix)}
		texts = append(texts, t.Spec.Tools...)
		return append(texts, ptrOr(t.Spec.Exclude)...)
	},
	columns: map[string]func(api.ToolSetSummary) sortValue{
		"name":      func(t api.ToolSetSummary) sortValue { return textValue(t.Meta.Name) },
		"server":    func(t api.ToolSetSummary) sortValue { return textValue(t.Spec.ServerRef) },
		"selection": func(t api.ToolSetSummary) sortValue { return textValue(strings.Join(t.Spec.Tools, ", ")) },
		"tools":     func(t api.ToolSetSummary) sortValue { return numValue(float64(t.Status.ResolvedToolCount)) },
		"valid":     func(t api.ToolSetSummary) sortValue { return conditionOf(t.Status.Conditions, "Valid") },
		"ready":     func(t api.ToolSetSummary) sortValue { return conditionOf(t.Status.Conditions, "Ready") },
	},
}

var capabilityListing = listing[api.CapabilitySummary]{
	name: func(c api.CapabilitySummary) string { return c.Meta.Name },
	texts: func(c api.CapabilitySummary) []string {
		return append([]string{c.Meta.Name, ptrOr(c.Spec.Description)}, c.Spec.ToolSets...)
	},
	columns: map[string]func(api.CapabilitySummary) sortValue{
		"name":        func(c api.CapabilitySummary) sortValue { return textValue(c.Meta.Name) },
		"description": func(c api.CapabilitySummary) sortValue { return optText(ptrOr(c.Spec.Description)) },
		"toolsets":    func(c api.CapabilitySummary) sortValue { return textValue(strings.Join(c.Spec.ToolSets, ", ")) },
		"valid":       func(c api.CapabilitySummary) sortValue { return conditionOf(c.Status.Conditions, "Valid") },
		"ready":       func(c api.CapabilitySummary) sortValue { return conditionOf(c.Status.Conditions, "Ready") },
	},
}

var grantListing = listing[api.GrantSummary]{
	name: func(g api.GrantSummary) string { return g.Meta.Name },
	texts: func(g api.GrantSummary) []string {
		return append([]string{g.Meta.Name, g.Spec.Subject}, g.Spec.Capabilities...)
	},
	columns: map[string]func(api.GrantSummary) sortValue{
		"name":         func(g api.GrantSummary) sortValue { return textValue(g.Meta.Name) },
		"subject":      func(g api.GrantSummary) sortValue { return textValue(g.Spec.Subject) },
		"capabilities": func(g api.GrantSummary) sortValue { return textValue(strings.Join(g.Spec.Capabilities, ", ")) },
		"valid":        func(g api.GrantSummary) sortValue { return conditionOf(g.Status.Conditions, "Valid") },
		"ready":        func(g api.GrantSummary) sortValue { return conditionOf(g.Status.Conditions, "Ready") },
	},
}

// subjectRow is a subject before it is resolved: what it is offered is
// worked out only for the rows a page shows, or for every row when the page
// is sorted by it.
type subjectRow struct {
	subject string
	grants  []string
	tools   func() int
}

var subjectListing = listing[subjectRow]{
	name:  func(r subjectRow) string { return r.subject },
	texts: func(r subjectRow) []string { return append([]string{r.subject}, r.grants...) },
	columns: map[string]func(subjectRow) sortValue{
		"name":   func(r subjectRow) sortValue { return textValue(r.subject) },
		"grants": func(r subjectRow) sortValue { return textValue(strings.Join(r.grants, ", ")) },
		"tools":  func(r subjectRow) sortValue { return numValue(float64(r.tools())) },
	},
}
