package dash

// The window, read once per frame into a profile that every part of a page asks,
// instead of each comparing w and h with its own number (docs/design/next/
// adaptive-layout.md). A size is a budget: the necessary is drawn first and what
// is left is spent in a fixed order, so nothing appears or vanishes at a magic
// number.

type widthClass int

const (
	widthCompact  widthClass = iota // under 100: a small or split window
	widthStandard                   // 100 to 159
	widthWide                       // 160 to 219: a detail beside the list
	widthGrand                      // 220 and up: a trend as well
)

type heightClass int

const (
	heightShort  heightClass = iota // under 28
	heightNormal                    // 28 to 47
	heightTall                      // 48 and up
)

// profile is the window as the layout reads it.
type profile struct {
	w, h int
	wc   widthClass
	hc   heightClass
}

func profileOf(w, h int) profile {
	p := profile{w: w, h: h}
	switch {
	case w >= 220:
		p.wc = widthGrand
	case w >= 160:
		p.wc = widthWide
	case w >= 100:
		p.wc = widthStandard
	}
	switch {
	case h >= 48:
		p.hc = heightTall
	case h >= 28:
		p.hc = heightNormal
	}
	return p
}

func (p profile) String() string {
	return [...]string{"compact", "standard", "wide", "grand"}[p.wc] + "/" + [...]string{"short", "normal", "tall"}[p.hc]
}

// nameCap is the widest a name is given: past it only the beginning tells
// instances apart.
func (p profile) nameCap() int {
	switch {
	case p.w >= 160:
		return 30
	case p.w >= 130:
		return 24
	case p.w >= 110:
		return 20
	}
	return 16
}

// Rows the budget counts, on Home.
const (
	titleRows     = 1
	meterRows     = 3 // mem, cpu, conns
	keyRows       = 2 // a line under each of mem and cpu, naming the colours
	gapRows       = 1 // the blank line under the header
	tableHead     = 1
	keyBarRows    = 1
	minLogRows    = 10 // the logs' box: eight lines and its borders
	keyWantsTable = 16 // the key is bought only while the table still shows this many rows
)

// keyedMeters says whether the line under each meter bar can be afforded: after
// it, the table still shows its rows (up to keyWantsTable) and the logs keep
// their minimum. It is the second upgrade, after the bars; the table's rows are
// not given up for it.
func (p profile) keyedMeters(tableRows int) bool {
	need := titleRows + meterRows + keyRows + gapRows + tableHead + min(tableRows, keyWantsTable) + keyBarRows + minLogRows
	return p.h >= need
}
