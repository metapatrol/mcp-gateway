package policy

import (
	"fmt"
	"testing"

	gatev1alpha1 "forgejo.metapatrol.local/platform/mcp-gate/api/v1alpha1"
)

// policyOfSize builds a policy the gateway resolves every tools/call against:
// servers with toolsPerServer tools each, a tool set per server selecting by
// exact names and one by pattern, capabilities combining capabilitySize tool
// sets, and one grant per subject naming two capabilities.
func policyOfSize(servers, toolsPerServer, capabilitySize, subjects int) (*Snapshot, Catalogs, string, string) {
	catalogs := map[string][]string{}
	var srv []gatev1alpha1.MCPServer
	var sets []gatev1alpha1.MCPToolSet
	for s := range servers {
		name := fmt.Sprintf("server-%d", s)
		srv = append(srv, server(name))
		var tools []string
		for i := range toolsPerServer {
			tools = append(tools, fmt.Sprintf("s%d_tool_%d", s, i))
		}
		catalogs[name] = tools
		sets = append(sets,
			toolSet(name+"-exact", name, tools[:min(5, len(tools))]...),
			toolSet(name+"-pattern", name, fmt.Sprintf("s%d_tool_*", s)))
		sets[len(sets)-1].Spec.Exclude = []string{fmt.Sprintf("s%d_tool_1?", s)}
	}
	var caps []gatev1alpha1.MCPCapability
	for c := 0; c+capabilitySize <= len(sets); c += capabilitySize {
		var names []string
		for _, ts := range sets[c : c+capabilitySize] {
			names = append(names, ts.Name)
		}
		caps = append(caps, capability(fmt.Sprintf("capability-%d", c), names...))
	}
	var grants []gatev1alpha1.MCPGrant
	for i := range subjects {
		subject := fmt.Sprintf("subject-%d", i)
		grants = append(grants, grantFor(subject, subject, caps[i%len(caps)].Name, caps[(i+1)%len(caps)].Name))
	}
	lookup := Catalogs(func(server string) ([]string, bool) {
		names, ok := catalogs[server]
		return names, ok
	})
	// tool 7 is selected by the pattern alone, the more expensive path.
	return NewSnapshot(srv, sets, caps, grants), lookup, "subject-0", catalogs["server-0"][7]
}

// BenchmarkAuthorize is the policy work of one tools/call.
func BenchmarkAuthorize(b *testing.B) {
	for _, size := range []struct {
		name                                             string
		servers, toolsPerServer, capabilitySize, subjects int
	}{
		{"small/3-servers-20-tools", 3, 20, 2, 10},
		{"medium/10-servers-50-tools", 10, 50, 4, 500},
		{"large/50-servers-200-tools", 50, 200, 10, 5000},
	} {
		b.Run(size.name, func(b *testing.B) {
			s, catalogs, subject, tool := policyOfSize(size.servers, size.toolsPerServer, size.capabilitySize, size.subjects)
			if _, ok := s.Authorize(subject, tool, catalogs); !ok {
				b.Fatalf("%s is not granted %s", subject, tool)
			}
			b.ReportAllocs()
			for b.Loop() {
				s.Authorize(subject, tool, catalogs)
			}
		})
	}
}
