package inspect

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// The list operations page, search and sort on the server, by the rules the
// console's tables follow (sortRows in @metapatrol/ng): text sorts naturally,
// numbers as numbers, items without a value go last whichever the direction,
// and ties fall back to the name, so the order is total and stable.

const maxLimit = 1000

// sortValue is the cell an item is sorted by: text, a number, or nothing.
type sortValue struct {
	Text  string  `json:"t,omitempty"`
	Num   float64 `json:"n,omitempty"`
	IsNum bool    `json:"i,omitempty"`
	Set   bool    `json:"s,omitempty"`
}

func textValue(s string) sortValue { return sortValue{Text: s, Set: true} }
func numValue(n float64) sortValue { return sortValue{Num: n, IsNum: true, Set: true} }
func optText(s string) sortValue   { return sortValue{Text: s, Set: s != ""} }
func optNum(n *int) sortValue      { return sortValue{Num: float64(ptrOr(n)), IsNum: true, Set: n != nil} }
func ptrOr[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return
}

// listing says how one kind of item is named, searched and sorted.
type listing[T any] struct {
	name    func(T) string
	texts   func(T) []string
	columns map[string]func(T) sortValue
}

// pageRequest is what a list operation was asked for.
type pageRequest struct {
	limit  *int
	cursor *string
	q      string
	sort   string
}

func newPageRequest[S ~string](limit *int, cursor, q *string, sort *S) pageRequest {
	p := pageRequest{limit: limit, cursor: cursor, sort: "name"}
	if q != nil {
		p.q = *q
	}
	if sort != nil && *sort != "" {
		p.sort = string(*sort)
	}
	return p
}

// invalidParam is a parameter the client got wrong; it is answered with 400.
type invalidParam struct{ msg string }

func (e invalidParam) Error() string { return e.msg }

// position is where a cursor continues: after the item with this sort value
// and name, for this q and sort.
type position struct {
	Q     string    `json:"q"`
	Sort  string    `json:"s"`
	After sortValue `json:"v"`
	Name  string    `json:"n"`
}

func encodeCursor(p position) string {
	b, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(c string) (position, error) {
	var p position
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		return p, invalidParam{"cursor is not one this API handed out"}
	}
	return p, nil
}

// page filters items by q, sorts them, and cuts the page the request asks
// for. total counts every item that matches q.
func page[T any](items []T, l listing[T], req pageRequest) (out []T, total int, next *string, err error) {
	if req.limit != nil && (*req.limit < 1 || *req.limit > maxLimit) {
		return nil, 0, nil, invalidParam{fmt.Sprintf("limit must be between 1 and %d", maxLimit)}
	}
	key, desc := strings.CutPrefix(req.sort, "-")
	column, ok := l.columns[key]
	if !ok {
		return nil, 0, nil, invalidParam{"cannot sort by " + req.sort}
	}

	matched := search(items, req.q, l.texts)
	total = len(matched)
	// Each value is computed once: a column may be expensive, as resolving a
	// subject is.
	type row struct {
		item  T
		value sortValue
		name  string
	}
	rows := make([]row, len(matched))
	for i, it := range matched {
		rows[i] = row{item: it, value: column(it), name: l.name(it)}
	}
	slices.SortStableFunc(rows, func(a, b row) int { return compareRows(a.value, b.value, a.name, b.name, desc) })

	start := 0
	if req.cursor != nil {
		pos, err := decodeCursor(*req.cursor)
		if err != nil {
			return nil, 0, nil, err
		}
		if pos.Q != req.q || pos.Sort != req.sort {
			return nil, 0, nil, invalidParam{"cursor was handed out for another q or sort"}
		}
		start, _ = slices.BinarySearchFunc(rows, pos, func(r row, p position) int {
			if compareRows(r.value, p.After, r.name, p.Name, desc) <= 0 {
				return -1
			}
			return 1
		})
	}
	end := len(rows)
	if req.limit != nil && start+*req.limit < end {
		end = start + *req.limit
		last := rows[end-1]
		c := encodeCursor(position{Q: req.q, Sort: req.sort, After: last.value, Name: last.name})
		next = &c
	}
	out = make([]T, 0, end-start)
	for _, r := range rows[start:end] {
		out = append(out, r.item)
	}
	return out, total, next, nil
}

// search keeps the items any of whose texts contain q, ignoring case.
func search[T any](items []T, q string, texts func(T) []string) []T {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return items
	}
	var out []T
	for _, it := range items {
		if slices.ContainsFunc(texts(it), func(t string) bool { return strings.Contains(strings.ToLower(t), q) }) {
			out = append(out, it)
		}
	}
	return out
}

func compareRows(x, y sortValue, nx, ny string, desc bool) int {
	if !x.Set || !y.Set {
		switch {
		case x.Set == y.Set:
			return natural(nx, ny)
		case !x.Set:
			return 1
		default:
			return -1
		}
	}
	var c int
	if x.IsNum && y.IsNum {
		c = cmp.Compare(x.Num, y.Num)
	} else {
		c = natural(x.Text, y.Text)
	}
	if c == 0 {
		return natural(nx, ny)
	}
	if desc {
		return -c
	}
	return c
}

// natural compares text the way people read it: runs of digits as numbers,
// so agent-2 comes before agent-10, and letters regardless of case, case
// deciding only between otherwise equal texts.
func natural(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			da := strings.TrimLeft(string(ra[si:i]), "0")
			db := strings.TrimLeft(string(rb[sj:j]), "0")
			if c := cmp.Compare(len(da), len(db)); c != 0 {
				return c
			}
			if c := strings.Compare(da, db); c != 0 {
				return c
			}
			continue
		}
		if c := cmp.Compare(unicode.ToLower(ra[i]), unicode.ToLower(rb[j])); c != 0 {
			return c
		}
		i++
		j++
	}
	if c := cmp.Compare(len(ra)-i, len(rb)-j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// conditionRank orders a condition False, then Unknown or absent, then True.
func conditionRank(status string) sortValue {
	switch status {
	case "False":
		return numValue(0)
	case "True":
		return numValue(2)
	}
	return numValue(1)
}
