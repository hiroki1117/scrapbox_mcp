package scrapbox

// editOp is the kind of a single step in a line edit script
type editOp int

const (
	opEqual editOp = iota
	opDelete
	opInsert
)

// edit is a single step of an edit script.
// oldIdx is valid for opEqual/opDelete, newIdx for opEqual/opInsert.
type edit struct {
	op     editOp
	oldIdx int
	newIdx int
}

// maxDiffDistance bounds the Myers search so that memory stays O(maxDiffDistance^2).
// When the pages differ more than this, the differing middle part is treated as
// a single replaced block.
var maxDiffDistance = 1000

// diffLines computes a minimal edit script that transforms a into b.
func diffLines(a, b []string) []edit {
	n, m := len(a), len(b)

	// Trim the common prefix and suffix; most edits touch only a small region
	prefix := 0
	for prefix < n && prefix < m && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && suffix < m-prefix && a[n-1-suffix] == b[m-1-suffix] {
		suffix++
	}

	edits := make([]edit, 0, n+m)
	for i := 0; i < prefix; i++ {
		edits = append(edits, edit{op: opEqual, oldIdx: i, newIdx: i})
	}

	midA, midB := a[prefix:n-suffix], b[prefix:m-suffix]
	mid, ok := myers(midA, midB)
	if !ok {
		// Too different: replace the whole middle block
		mid = mid[:0]
		for i := range midA {
			mid = append(mid, edit{op: opDelete, oldIdx: i})
		}
		for j := range midB {
			mid = append(mid, edit{op: opInsert, newIdx: j})
		}
	}
	for _, e := range mid {
		e.oldIdx += prefix
		e.newIdx += prefix
		edits = append(edits, e)
	}

	for i := 0; i < suffix; i++ {
		edits = append(edits, edit{op: opEqual, oldIdx: n - suffix + i, newIdx: m - suffix + i})
	}
	return edits
}

// myers implements the Myers O(ND) diff algorithm.
// It returns false if the edit distance exceeds maxDiffDistance.
func myers(a, b []string) ([]edit, bool) {
	n, m := len(a), len(b)
	maxD := n + m
	if maxD > maxDiffDistance {
		maxD = maxDiffDistance
	}

	// v[k+off] is the furthest x reached on diagonal k
	off := maxD + 1
	v := make([]int, 2*maxD+3)
	// trace[d] holds v[-d..d] as it was before step d, for backtracking
	var trace [][]int

	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, 2*d+1)
		copy(snapshot, v[off-d:off+d+1])
		trace = append(trace, snapshot)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // move down (insert)
			} else {
				x = v[off+k-1] + 1 // move right (delete)
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m), true
			}
		}
	}
	return nil, false
}

// backtrack rebuilds the edit script from the Myers trace
func backtrack(trace [][]int, n, m int) []edit {
	var reversed []edit
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		t := trace[d]
		get := func(k int) int { return t[k+d] }

		k := x - y
		var prevK int
		if k == -d || (k != d && get(k-1) < get(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := get(prevK)
		prevY := prevX - prevK

		for x > prevX && y > prevY {
			reversed = append(reversed, edit{op: opEqual, oldIdx: x - 1, newIdx: y - 1})
			x--
			y--
		}
		if x == prevX {
			reversed = append(reversed, edit{op: opInsert, newIdx: y - 1})
		} else {
			reversed = append(reversed, edit{op: opDelete, oldIdx: x - 1})
		}
		x, y = prevX, prevY
	}
	for x > 0 && y > 0 {
		reversed = append(reversed, edit{op: opEqual, oldIdx: x - 1, newIdx: y - 1})
		x--
		y--
	}

	edits := make([]edit, len(reversed))
	for i, e := range reversed {
		edits[len(reversed)-1-i] = e
	}
	return edits
}
