// The kafka engine page: topics · groups · partition flow. Lag is the
// headline — "is my consumer keeping up?" is the one Kafka question local dev
// actually asks — so groups sit at eye level with a trend, and sustained
// growth promotes itself to the attention line. Rates are computed dash-side
// from successive Admin samples (high-water deltas), so the module stays a
// dumb reporter.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/doze-dev/doze/internal/control"
)

// ksample is one polled value with its arrival time.
type ksample struct {
	t time.Time
	v int64
}

const khistMax = 70 // ~1min of 1s samples

// kpush appends a sample to a named series, capped.
func (m *model) kpush(key string, v int64) {
	s := append(m.khist[key], ksample{time.Now(), v})
	if len(s) > khistMax {
		s = s[len(s)-khistMax:]
	}
	m.khist[key] = s
}

// krate returns the series' delta over the last minute (0 if too little data),
// plus per-bucket deltas for a sparkline.
func krate(s []ksample, buckets int) (perMin int64, spark []int) {
	spark = make([]int, buckets)
	if len(s) < 2 {
		return 0, spark
	}
	now := time.Now()
	window := time.Minute
	first := s[0]
	for _, x := range s {
		if now.Sub(x.t) <= window {
			first = x
			break
		}
	}
	last := s[len(s)-1]
	perMin = last.v - first.v
	if perMin < 0 {
		perMin = 0 // topic recreated / counter reset
	}
	for i := 1; i < len(s); i++ {
		d := s[i].v - s[i-1].v
		if d <= 0 {
			continue
		}
		age := now.Sub(s[i].t)
		if age < 0 || age >= window {
			continue
		}
		b := buckets - 1 - int(age*time.Duration(buckets)/window)
		if b >= 0 && b < buckets {
			spark[b] += int(d)
		}
	}
	return perMin, spark
}

// ktrend classifies a lag series: rising (still growing at the tail), falling,
// or steady — plus for how long it has been rising.
func ktrend(s []ksample) (arrow string, risingFor time.Duration) {
	if len(s) < 3 {
		return "", 0
	}
	last := s[len(s)-1].v
	prev := s[len(s)-2].v
	switch {
	case last > prev:
		// walk back to when the rise started
		start := len(s) - 1
		for start > 0 && s[start].v >= s[start-1].v {
			start--
		}
		return "▲", time.Since(s[start].t)
	case last < prev:
		return "▼", 0
	default:
		return "", 0
	}
}

// recordKafkaSamples feeds the history from a fresh Admin snapshot.
func (m *model) recordKafkaSamples(name string, res []control.ResourceView) {
	if m.khist == nil {
		m.khist = map[string][]ksample{}
	}
	for _, r := range res {
		switch r.Kind {
		case "topic":
			if h, err := strconv.ParseInt(r.Info["high"], 10, 64); err == nil {
				m.kpush("t:"+name+":"+r.Name, h)
			}
		case "group":
			if l, err := strconv.ParseInt(r.Info["lag"], 10, 64); err == nil {
				m.kpush("g:"+name+":"+r.Name, l)
			}
		}
	}
}

// viewKafkaPage renders the kafka body into the logs-pane region.
func (m model) viewKafkaPage(v control.InstanceView, w int) string {
	height := max(3, m.bodyH()-m.detailBoxH()-1)
	head := func(title, extra string) string {
		t := stLabel.Render(title)
		if extra != "" {
			t += stDim.Render(" · " + extra)
		}
		fill := max(1, w-4-lipgloss.Width(t))
		return stFaint.Render("╌╌ ") + t + " " + stFaint.Render(strings.Repeat("╌", fill))
	}
	var lines []string

	if m.adminName != v.Name || len(m.adminRes) == 0 {
		msg := "reading the broker…"
		if v.PID == 0 {
			msg = "asleep — enter wakes it, then topics fill in"
		} else if m.adminErr != "" {
			msg = "broker unreadable (" + m.adminErr + ") — l shows raw logs"
		}
		lines = append(lines, head("kafka", ""), stDim.Render("  "+msg))
		return pad(lines, height)
	}

	var topics, groups []control.ResourceView
	for _, r := range m.adminRes {
		switch r.Kind {
		case "topic":
			topics = append(topics, r)
		case "group":
			groups = append(groups, r)
		}
	}

	// ── topics ──
	focusHint := "tab focuses · l logs"
	if m.boardFocus {
		focusHint = "j/k move · enter copies the bootstrap address · esc back"
	}
	lines = append(lines, head("topics", focusHint))
	for i, t := range topics {
		bar := "  "
		if m.boardFocus && i == m.boardSel {
			bar = stAccent.Render("▌ ")
		}
		perMin, spark := krate(m.khist["t:"+v.Name+":"+t.Name], 8)
		maxB := 1
		for _, b := range spark {
			if b > maxB {
				maxB = b
			}
		}
		rate := stDim.Render("quiet")
		if perMin > 0 {
			rate = stDim.Render(plural(int(perMin), "msg") + " · 1m")
		}
		row := fmt.Sprintf("%s%-16s %-4s partitions   high-water %-10s %s %s",
			bar, t.Name, t.Info["partitions"], t.Info["high"],
			lipgloss.NewStyle().Foreground(cCyan).Render(sparkline(spark, 8, maxB)),
			rate)
		if m.boardFocus && i == m.boardSel {
			row = lipgloss.NewStyle().Background(cSel).Render(row)
		}
		lines = append(lines, truncate(row, w-2))
	}

	// ── groups ──
	var attention []string
	if len(groups) > 0 {
		lines = append(lines, head("groups", "lag is the number that matters"))
		for _, g := range groups {
			lag := g.Info["lag"]
			arrow, rising := ktrend(m.khist["g:"+v.Name+":"+g.Name])
			lagStyle := stGreen
			note := ""
			if lag != "" && lag != "0" {
				lagStyle = lipgloss.NewStyle().Foreground(cGold)
				if arrow == "▲" && rising > 20*time.Second {
					note = stDim.Render("  growing for " + shortDurTUI(rising))
					attention = append(attention,
						fmt.Sprintf("group %s lag %s and rising — consumer slower than the produce rate", g.Name, lag))
				}
			}
			if lag == "" {
				lag = "—"
			}
			row := fmt.Sprintf("    %-16s %-3s members   %-10s lag %s %s%s",
				g.Name, g.Info["members"], g.Info["state"],
				lagStyle.Render(lag), lagStyle.Render(arrow), note)
			lines = append(lines, truncate(row, w-2))
		}
	}

	for _, a := range attention {
		lines = append(lines, lipgloss.NewStyle().Foreground(cGold).Render("  ⚠ "+truncate(a, w-6)))
	}

	// ── connect — the console for browsing, the bootstrap for clients ──
	addr := connectLine(v)
	topic := "<topic>"
	if len(topics) > 0 {
		topic = topics[min(m.boardSel, len(topics)-1)].Name
	}
	lines = append(lines, head("connect", ""))
	if cons := m.webURL(v); cons != "" {
		lines = append(lines,
			"    "+stDim.Render("console     ")+stLabel.Render(cons)+stDim.Render("   o opens it"))
	}
	lines = append(lines,
		"    "+stDim.Render("bootstrap   ")+stLabel.Render(addr)+stDim.Render("   (KAFKA_BROKERS in doze env)"),
		"    "+stDim.Render("consume     kcat -b "+addr+" -t "+topic+" -C"))

	return pad(lines, height)
}
