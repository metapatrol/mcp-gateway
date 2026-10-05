package inspect

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestNatural(t *testing.T) {
	names := []string{"agent-10", "Agent-3", "agent-2", "agent", "agent-02b", "b", "a-1"}
	slices.SortFunc(names, natural)
	assert.Equal(t, []string{"a-1", "agent", "agent-2", "agent-02b", "Agent-3", "agent-10", "b"}, names)
}

type item struct {
	name  string
	count *int
}

var items = listing[item]{
	name:  func(i item) string { return i.name },
	texts: func(i item) []string { return []string{i.name} },
	columns: map[string]func(item) sortValue{
		"name":  func(i item) sortValue { return textValue(i.name) },
		"count": func(i item) sortValue { return optNum(i.count) },
	},
}

func names(page []item) []string {
	out := []string{}
	for _, i := range page {
		out = append(out, i.name)
	}
	return out
}

func TestPageSortsLikeTheConsole(t *testing.T) {
	all := []item{{"c", ptr.To(1)}, {"a", nil}, {"b", ptr.To(1)}, {"d", ptr.To(5)}}

	asc, total, next, err := page(all, items, pageRequest{sort: "count"})
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "c", "d", "a"}, names(asc), "ties by name, no value last")
	assert.Equal(t, 4, total)
	assert.Nil(t, next, "no limit, one page")

	desc, _, _, err := page(all, items, pageRequest{sort: "-count"})
	require.NoError(t, err)
	assert.Equal(t, []string{"d", "b", "c", "a"}, names(desc), "ties stay by name, no value stays last")
}

func TestPageWalksWithCursors(t *testing.T) {
	var all []item
	for i := 1; i <= 7; i++ {
		all = append(all, item{name: fmt.Sprintf("agent-%d", i)})
	}
	req := pageRequest{limit: ptr.To(3), sort: "-name"}

	var seen []string
	for range 4 {
		p, total, next, err := page(all, items, req)
		require.NoError(t, err)
		assert.Equal(t, 7, total)
		seen = append(seen, names(p)...)
		if next == nil {
			break
		}
		req.cursor = next
	}
	assert.Equal(t, []string{"agent-7", "agent-6", "agent-5", "agent-4", "agent-3", "agent-2", "agent-1"}, seen)
}

func TestCursorSurvivesChanges(t *testing.T) {
	all := []item{{name: "a"}, {name: "b"}, {name: "c"}, {name: "d"}}
	first, _, next, err := page(all, items, pageRequest{limit: ptr.To(2), sort: "name"})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, names(first))

	// b, the item the cursor names, goes away, and one is added before it:
	// the next page starts after b all the same.
	changed := []item{{name: "a"}, {name: "aa"}, {name: "c"}, {name: "d"}}
	second, total, _, err := page(changed, items, pageRequest{limit: ptr.To(2), cursor: next, sort: "name"})
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "d"}, names(second))
	assert.Equal(t, 4, total)
}

func TestPageSearches(t *testing.T) {
	all := []item{{name: "Agent-Coder"}, {name: "support"}}
	p, total, _, err := page(all, items, pageRequest{q: " coder ", sort: "name"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Agent-Coder"}, names(p))
	assert.Equal(t, 1, total)
}

func TestPageRefusesWhatItDidNotHandOut(t *testing.T) {
	all := []item{{name: "a"}, {name: "b"}}
	_, _, next, err := page(all, items, pageRequest{limit: ptr.To(1), sort: "name"})
	require.NoError(t, err)

	for name, req := range map[string]pageRequest{
		"limit 0":        {limit: ptr.To(0), sort: "name"},
		"limit too high": {limit: ptr.To(maxLimit + 1), sort: "name"},
		"unknown sort":   {sort: "colour"},
		"garbage cursor": {cursor: ptr.To("%%%"), sort: "name"},
		"another sort":   {cursor: next, sort: "-name"},
		"another q":      {cursor: next, q: "a", sort: "name"},
	} {
		_, _, _, err := page(all, items, req)
		_, bad := isInvalidParam(err)
		assert.True(t, bad, name)
	}
}
