// Package layout is the one description of where things are on a screen.
// A page computes its Regions from the size it is given, and then both
// draws from them and answers a click from them, so what is drawn and what
// is clickable cannot drift apart.
package layout

// Rect is a rectangle of cells.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the cell (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Empty reports whether r has no cells.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Region is a named rectangle: a row, a tab, a button.
type Region struct {
	ID   string
	Rect Rect
}

// Map is the regions of one screen, in the order they were added. Later
// regions are on top of earlier ones.
type Map struct{ regions []Region }

// Add puts a region on the screen.
func (m *Map) Add(id string, r Rect) {
	if !r.Empty() {
		m.regions = append(m.regions, Region{ID: id, Rect: r})
	}
}

// At returns the ID of the topmost region holding the cell (x, y).
func (m Map) At(x, y int) (string, bool) {
	for i := len(m.regions) - 1; i >= 0; i-- {
		if m.regions[i].Rect.Contains(x, y) {
			return m.regions[i].ID, true
		}
	}
	return "", false
}

// Regions returns every region, for a test to look at.
func (m Map) Regions() []Region { return append([]Region(nil), m.regions...) }

// SplitLeft cuts the left w cells off r.
func SplitLeft(r Rect, w int) (left, rest Rect) {
	if w > r.W {
		w = r.W
	}
	return Rect{r.X, r.Y, w, r.H}, Rect{r.X + w, r.Y, r.W - w, r.H}
}

// SplitTop cuts the top h rows off r.
func SplitTop(r Rect, h int) (top, rest Rect) {
	if h > r.H {
		h = r.H
	}
	return Rect{r.X, r.Y, r.W, h}, Rect{r.X, r.Y + h, r.W, r.H - h}
}

// SplitBottom cuts the bottom h rows off r.
func SplitBottom(r Rect, h int) (rest, bottom Rect) {
	if h > r.H {
		h = r.H
	}
	return Rect{r.X, r.Y, r.W, r.H - h}, Rect{r.X, r.Y + r.H - h, r.W, h}
}
