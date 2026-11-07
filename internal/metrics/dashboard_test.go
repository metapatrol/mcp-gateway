package metrics

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dashboardPath is the dashboard the grafana chart ships.
const dashboardPath = "../../charts/mcp-gateway-grafana-dashboard/dashboards/mcp-gateway.json"

// The dashboard and the metrics live in different files and nothing ties them
// together: a renamed metric compiles, passes every other test, and leaves a
// panel that silently shows no data. This reads every mcp_gateway_* name the
// dashboard queries and checks that this package still registers it.
func TestDashboardQueriesOnlyRegisteredMetrics(t *testing.T) {
	raw, err := os.ReadFile(dashboardPath)
	require.NoError(t, err)

	var dashboard struct {
		Panels []struct {
			Title   string `json:"title"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
		Templating struct {
			List []struct {
				Query any `json:"query"`
			} `json:"list"`
		} `json:"templating"`
	}
	require.NoError(t, json.Unmarshal(raw, &dashboard))

	registered := registeredNames()
	name := regexp.MustCompile(`mcp_gateway_[a-z_]+`)
	used := map[string]bool{}

	check := func(where, expr string) {
		for _, m := range name.FindAllString(expr, -1) {
			used[m] = true
			assert.True(t, registered[m], "%s queries %s, which this package does not register", where, m)
		}
	}
	for _, p := range dashboard.Panels {
		for _, target := range p.Targets {
			check("panel "+p.Title, target.Expr)
		}
	}
	for _, v := range dashboard.Templating.List {
		if q, ok := v.Query.(string); ok {
			check("variable query", q)
		}
	}
	assert.NotEmpty(t, used, "the dashboard queries no mcp_gateway metric at all")
}

// registeredNames returns the series names the package's collectors produce. A
// histogram appears under its _bucket, _sum and _count names, which are what a
// query actually selects.
func registeredNames() map[string]bool {
	names := map[string]bool{}
	collectors := []prometheus.Collector{
		BuildInfo, Resources, Discoveries, DiscoveredTools, BackendAvailable,
		ToolCalls, ToolCallDuration, SubjectToolCalls, DeniedToolCalls, AuthFailures,
		InspectRequests, InspectRequestDuration, InspectEventStreams, InspectAuthFailures,
	}
	fqName := regexp.MustCompile(`fqName: "([^"]+)"`)
	for _, c := range collectors {
		descs := make(chan *prometheus.Desc, 1)
		go func() {
			c.Describe(descs)
			close(descs)
		}()
		for d := range descs {
			m := fqName.FindStringSubmatch(d.String())
			if m == nil {
				continue
			}
			names[m[1]] = true
			if _, isHistogram := c.(*prometheus.HistogramVec); isHistogram {
				for _, suffix := range []string{"_bucket", "_sum", "_count"} {
					names[m[1]+suffix] = true
				}
			}
		}
	}
	// Guard the parsing above: if Desc's String format ever changes, every
	// name would go missing and the test would fail for the wrong reason.
	if !names["mcp_gateway_tool_calls_total"] {
		panic("could not read metric names from prometheus.Desc: " + strings.TrimSpace(ToolCalls.WithLabelValues("", "", "").Desc().String()))
	}
	return names
}
