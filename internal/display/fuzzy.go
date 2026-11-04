package display

import "strings"

// How the palette filters: each word typed must begin a word of the label, in
// order. "re" finds restart, reset and rerun and not "wake worker", whose
// letters are there but nowhere a word starts; "reset d" finds reset db.

// FuzzyMatch reports whether query matches s by word starts, and the positions
// in s of the letters matched, for drawing them. An empty query matches.
func FuzzyMatch(query, s string) ([]int, bool) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, true
	}
	rs := []rune(strings.ToLower(s))
	var at []int
	pos, t := 0, 0
	for t < len(terms) {
		found := false
		for p := pos; p < len(rs); p++ {
			if p > 0 && rs[p-1] != ' ' {
				continue
			}
			term := []rune(terms[t])
			if p+len(term) <= len(rs) && string(rs[p:p+len(term)]) == terms[t] {
				for i := range term {
					at = append(at, p+i)
				}
				pos, found = p+len(term), true
				break
			}
		}
		if !found {
			return nil, false
		}
		t++
	}
	return at, true
}

// FuzzyScore ranks matches: lower is better. Letters together, and near the
// front, first.
func FuzzyScore(at []int) int {
	if len(at) == 0 {
		return 0
	}
	n := at[0]
	for i := 1; i < len(at); i++ {
		n += at[i] - at[i-1] - 1
	}
	return n
}
