package scrapbox

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

const testUserID = "0123456789abcdef01234567"

// applyChanges simulates how Scrapbox applies commit changes to a page.
// `_insert: X` inserts the new line immediately BEFORE line X ("_end" appends).
func applyChanges(t *testing.T, lines []Line, changes []map[string]interface{}) []Line {
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
	return page
}

func texts(lines []Line) []string {
	result := make([]string, len(lines))
	for i, l := range lines {
		result[i] = l.Text
	}
	return result
}

func makeLines(texts ...string) []Line {
	lines := make([]Line, len(texts))
	for i, text := range texts {
		lines[i] = Line{ID: fmt.Sprintf("old%d", i), Text: text}
	}
	return lines
}

// countOps counts changes by kind (_insert, _update, _delete)
func countOps(changes []map[string]interface{}) map[string]int {
	counts := map[string]int{}
	for _, c := range changes {
		for _, op := range []string{"_insert", "_update", "_delete"} {
			if _, ok := c[op]; ok {
				counts[op]++
			}
		}
	}
	return counts
}

// idOf returns the ID of the first line with the given text
func idOf(t *testing.T, lines []Line, text string) string {
	t.Helper()
	for _, l := range lines {
		if l.Text == text {
			return l.ID
		}
	}
	t.Fatalf("text not found: %q", text)
	return ""
}

func TestDiffToChanges_ResultMatches(t *testing.T) {
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
		{"to single line", []string{"title", "a", "b"}, []string{"title"}},
		{"duplicate lines", []string{"t", "", "a", "", "a", ""}, []string{"t", "", "a", "x", "", "a", ""}},
		{"prepend after title", []string{"t", "a", "b"}, []string{"t", "x", "y", "a", "b"}},
		{"full rewrite", []string{"t", "a", "b", "c"}, []string{"u", "v", "w"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := makeLines(c.old...)
			changes := diffToChanges(old, c.newTexts, testUserID)
			if got := texts(applyChanges(t, old, changes)); !reflect.DeepEqual(got, c.newTexts) {
				t.Errorf("got %q, want %q", got, c.newTexts)
			}
		})
	}
}

func TestDiffToChanges_NoChanges(t *testing.T) {
	old := makeLines("t", "a", "b")
	if changes := diffToChanges(old, []string{"t", "a", "b"}, testUserID); len(changes) != 0 {
		t.Errorf("expected no changes, got %v", changes)
	}
}

// Inserting lines in the middle must not touch the following lines
func TestDiffToChanges_InsertInMiddleKeepsFollowingLines(t *testing.T) {
	old := makeLines("title", "A-1", "A-2", "見出しB", "B-1", "B-2")
	newTexts := []string{"title", "A-1", "A-2", "A-3", "A-4", "見出しB", "B-1", "B-2"}

	changes := diffToChanges(old, newTexts, testUserID)
	if got := countOps(changes); !reflect.DeepEqual(got, map[string]int{"_insert": 2}) {
		t.Fatalf("expected only 2 inserts, got %v", got)
	}

	result := applyChanges(t, old, changes)
	if got := texts(result); !reflect.DeepEqual(got, newTexts) {
		t.Fatalf("got %q, want %q", got, newTexts)
	}
	for i, text := range []string{"見出しB", "B-1", "B-2"} {
		if id := idOf(t, result, text); id != old[3+i].ID {
			t.Errorf("%q should keep its line ID %s, got %s", text, old[3+i].ID, id)
		}
	}
}

// Editing one line and deleting another must not touch the other lines
func TestDiffToChanges_EditAndDeleteAreMinimal(t *testing.T) {
	old := makeLines("title", "a", "b", "c", "d", "e", "f", "g", "h", "i")
	newTexts := []string{"title", "A", "b", "d", "e", "f", "g", "h", "i"} // edit a, delete c

	changes := diffToChanges(old, newTexts, testUserID)
	if got := countOps(changes); !reflect.DeepEqual(got, map[string]int{"_update": 1, "_delete": 1}) {
		t.Fatalf("expected 1 update and 1 delete, got %v (%v)", got, changes)
	}
	if got := texts(applyChanges(t, old, changes)); !reflect.DeepEqual(got, newTexts) {
		t.Errorf("got %q, want %q", got, newTexts)
	}
}

// Random edits must always produce the expected text, both with the Myers
// search and with the fallback used for very different pages
func TestDiffToChanges_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []string{"", "a", "b", "c", "d"}
	randomTexts := func(n int) []string {
		result := make([]string, n)
		for i := range result {
			result[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return result
	}

	for _, limit := range []int{maxDiffDistance, 3} {
		t.Run(fmt.Sprintf("maxDiffDistance=%d", limit), func(t *testing.T) {
			defer func(orig int) { maxDiffDistance = orig }(maxDiffDistance)
			maxDiffDistance = limit

			for i := 0; i < 500; i++ {
				old := makeLines(randomTexts(rng.Intn(15))...)
				newTexts := randomTexts(rng.Intn(15))
				changes := diffToChanges(old, newTexts, testUserID)
				if got := texts(applyChanges(t, old, changes)); !reflect.DeepEqual(got, newTexts) {
					t.Fatalf("old %q -> new %q: got %q", texts(old), newTexts, got)
				}
			}
		})
	}
}

func TestDiffLines_IsMinimal(t *testing.T) {
	a := []string{"a", "b", "c", "a", "b", "b", "a"}
	b := []string{"c", "b", "a", "b", "a", "c"}
	changed := 0
	for _, e := range diffLines(a, b) {
		if e.op != opEqual {
			changed++
		}
	}
	// The classic example from the Myers paper has edit distance 5
	if changed != 5 {
		t.Errorf("expected edit distance 5, got %d", changed)
	}
}

func TestInsertLinesChanges(t *testing.T) {
	old := makeLines("title", "A-1", "A-2", "見出しB", "B-1")
	cases := []struct {
		name   string
		target string
		want   []string
	}{
		{"after middle line", "A-2", []string{"title", "A-1", "A-2", "x", "y", "見出しB", "B-1"}},
		{"after last line", "B-1", []string{"title", "A-1", "A-2", "見出しB", "B-1", "x", "y"}},
		{"empty target appends", "", []string{"title", "A-1", "A-2", "見出しB", "B-1", "x", "y"}},
		{"missing target appends", "nothing", []string{"title", "A-1", "A-2", "見出しB", "B-1", "x", "y"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			changes := insertLinesChanges(old, c.target, []string{"x", "y"}, testUserID)
			if got := countOps(changes); !reflect.DeepEqual(got, map[string]int{"_insert": 2}) {
				t.Fatalf("expected only 2 inserts, got %v", got)
			}
			result := applyChanges(t, old, changes)
			if got := texts(result); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
			for _, line := range old {
				if id := idOf(t, result, line.Text); id != line.ID {
					t.Errorf("%q should keep its line ID", line.Text)
				}
			}
		})
	}
}
