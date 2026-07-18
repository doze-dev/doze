package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/doze-dev/doze/internal/control"
)

// TestRenderGallery renders every screen at the minimum and default window
// sizes so the whole dash can be eyeballed without a daemon:
//
//	go test ./internal/tui -run Gallery -v
//
// Beyond the eyeball, it asserts the one invariant that matters at every size:
// no rendered line may exceed the window width (overflow makes the terminal
// scroll and tears the frame).
func TestRenderGallery(t *testing.T) {
	sizes := []struct{ w, h int }{{64, 18}, {110, 32}}

	home := func() model {
		m := threeInstances()
		m.resp.Instances[0].PID = 4321
		m.resp.Instances[0].Conns = 3
		m.resp.Instances[0].RAM = 42 * 1024 * 1024
		m.resp.Instances[0].CPU = 12
		m.resp.Instances[0].URL = "postgres://app@app-dev.demo.doze:5432/app_dev?sslmode=disable"
		m.resp.Instances[0].Endpoint = "127.0.0.11:5432"
		m.resp.Instances[0].Bind = "127.0.0.11:5432"
		m.resp.Instances[0].Domain = "app-dev.demo.doze"
		m.resp.Instances[0].EnvVar = "DATABASE_URL"
		h := &history{}
		// A moving series so the braille charts render as real lines: a memory
		// ramp with a sine wobble, cpu bursts, connections stepping.
		for i := range 120 {
			ram := 30 + 12*math.Sin(float64(i)/14) + float64(i)/8
			cpu := 6 + 5*math.Sin(float64(i)/9)
			conns := 2 + float64((i/30)%3)
			h.push(ram*1024*1024, cpu, conns)
		}
		m.hist["app"] = h
		m.logVP.SetContent("ready to accept connections\nlistening on 127.0.0.11:5432")
		return m
	}

	steady := func() model { // flat series must read as words, not noise
		m := home()
		h := &history{}
		for range 40 {
			h.push(42*1024*1024, 2, 3)
		}
		m.hist["app"] = h
		return m
	}

	awsStrip := func() model { // an s3 builtin with a resource strip + console door
		m := home()
		m.resp.Instances = append(m.resp.Instances,
			control.InstanceView{Name: "console", Engine: "aws-console", State: "active",
				PID: 77, URL: "http://console.demo.doze"},
		)
		m.resp.Instances[2].PID = 9
		m.resp.Instances[2].State = "active"
		m.resp.Instances[2].LastError = ""
		m.resp.Instances[2].RAM = 18 * 1024 * 1024
		m.resp.Instances[2].Resource = "http://s3.demo.doze"
		m.cursor = 2 // the s3 "media" instance
		h := &history{}
		for range 40 {
			h.push(18*1024*1024, 1, 2)
		}
		m.hist["media"] = h
		m.adminName = "media"
		m.adminRes = []control.ResourceView{
			{Kind: "bucket", Name: "uploads", Status: "18 objects · 4.2 MB"},
			{Kind: "bucket", Name: "avatars", Status: "112 objects · 9.8 MB"},
		}
		return m
	}

	screens := []struct {
		name string
		mk   func() model
	}{
		{"home", home},
		{"home-steady", steady},
		{"home-aws-strip", awsStrip},
		{"palette", func() model {
			m := home()
			m.paletteMode = true
			return m
		}},
		{"confirm", func() model {
			m := home()
			m.dashPending = "down:app"
			return m
		}},
		{"help", func() model {
			m := home()
			m.showHelp = true
			return m
		}},
		// Stress fixtures: each of these caught a real frame-tearing overflow —
		// a crowded fleet (sidebar windowing), hostile name/listen lengths
		// (header + confirm clamps), and copy mode.
		{"home-crowd", func() model {
			m := home()
			for i := range 30 {
				m.resp.Instances = append(m.resp.Instances,
					control.InstanceView{Name: fmt.Sprintf("svc-%02d", i), Engine: "postgres", State: "reaped"})
			}
			m.cursor = len(m.resp.Instances) - 1 // the selection must stay on screen
			return m
		}},
		{"home-hostile", func() model {
			m := home()
			m.resp.Listen = "127.0.0.1:6432 and an implausibly long listen banner that must never widen the header line"
			m.resp.Instances[0].Name = strings.Repeat("very-long-name-", 8)
			return m
		}},
		{"confirm-longname", func() model {
			m := home()
			m.resp.Instances[0].Name = strings.Repeat("very-long-name-", 8)
			m.dashPending = "down:" + m.resp.Instances[0].Name
			return m
		}},
		{"copy", func() model {
			m := home()
			m.logLines = []string{"ready to accept connections", "listening on 127.0.0.11:5432", "checkpoint complete"}
			m.copyMode, m.copyLines, m.copyCursor = true, m.logLines, 2
			return m
		}},
	}

	for _, size := range sizes {
		for _, sc := range screens {
			t.Run(fmt.Sprintf("%s-%dx%d", sc.name, size.w, size.h), func(t *testing.T) {
				m := sc.mk()
				m.width, m.height = size.w, size.h
				m.layout()
				out := m.View()
				lines := strings.Split(out, "\n")
				// Both axes matter: an overwide line makes the terminal scroll
				// sideways; an overtall frame scrolls it vertically. Either tears.
				if len(lines) > size.h {
					t.Errorf("frame overflows: %d rows > %d", len(lines), size.h)
				}
				for i, ln := range lines {
					if w := ansi.StringWidth(ln); w > size.w {
						t.Errorf("line %d overflows: %d > %d\n%s", i, w, size.w, ln)
					}
				}
				t.Logf("%s @ %dx%d:\n%s", sc.name, size.w, size.h, out)
			})
		}
	}
}
