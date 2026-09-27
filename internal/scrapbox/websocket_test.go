package scrapbox

import (
	"fmt"
	"reflect"
	"testing"
)

// applyChanges simulates how Scrapbox applies commit changes to a page.
// `_insert: X` inserts the new line immediately BEFORE line X ("_end" appends).
func applyChanges(t *testing.T, lines []Line, changes []map[string]interface{}) []string {
	t.Helper()
	page := append([]Line(nil), lines...)
	indexOf := func(id string) int {
		for i, l := range page {
			if l.ID == id {
				return i
			}
		}
		t.Fatalf("line not found: %q", id)
		return -1
	}
	for _, c := range changes {
		switch {
		case c["_insert"] != nil:
			ln := c["lines"].(map[string]interface{})
			newLine := Line{ID: ln["id"].(string), Text: ln["text"].(string)}
			pos := len(page)
			if target := c["_insert"].(string); target != "_end" {
				pos = indexOf(target)
			}
			page = append(page[:pos], append([]Line{newLine}, page[pos:]...)...)
		case c["_update"] != nil:
			i := indexOf(c["_update"].(string))
			page[i].Text = c["lines"].(map[string]interface{})["text"].(string)
		case c["_delete"] != nil:
			i := indexOf(c["_delete"].(string))
			page = append(page[:i], page[i+1:]...)
		}
	}
	texts := make([]string, len(page))
	for i, l := range page {
		texts[i] = l.Text
	}
	return texts
}

func makeLines(texts ...string) []Line {
	lines := make([]Line, len(texts))
	for i, text := range texts {
		lines[i] = Line{ID: fmt.Sprintf("old%d", i), Text: text}
	}
	return lines
}

func TestDiffToChanges(t *testing.T) {
	cases := []struct {
		name     string
		old      []string
		newTexts []string
	}{
		{"same length update", []string{"title", "a", "b"}, []string{"title", "A", "b"}},
		{"grow keeps order", []string{"title", "a", "related"}, []string{"title", "a", "b", "c", "d", "related"}},
		{"append only", []string{"title", "a"}, []string{"title", "a", "b", "c"}},
		{"shrink", []string{"title", "a", "b", "c"}, []string{"title", "x"}},
		{"from empty page", nil, []string{"title", "a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := makeLines(c.old...)
			changes := diffToChanges(old, c.newTexts, "0123456789abcdef01234567")
			got := applyChanges(t, old, changes)
			if !reflect.DeepEqual(got, c.newTexts) {
				t.Errorf("got %q, want %q", got, c.newTexts)
			}
		})
	}
}
