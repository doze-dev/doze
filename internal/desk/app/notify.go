//go:build purego

package app

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/doze-dev/doze/internal/desk/notify"
	"github.com/doze-dev/doze/internal/desk/view"
)

// Notifications outside the window, through the system's own (D8). macOS
// delivers them only for an app in a bundle, so a bare binary (the desk
// bench, a dev build run from the shell) logs them to stderr instead.

// inBundle says this binary runs from inside a .app, where the system
// notification centre will take it.
func inBundle() bool {
	if runtime.GOOS != "darwin" {
		return true // Linux notifies over D-Bus from any binary
	}
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

// notifier is the service that sends, when there is one.
type notifier struct {
	ns *notifications.NotificationService
	d  *desk
}

// newNotifier makes the Wails notifications service when it can be used;
// the services to register are empty otherwise.
func newNotifier() (*notifier, []application.Service) {
	if !inBundle() {
		return &notifier{}, nil
	}
	ns := notifications.New()
	return &notifier{ns: ns}, []application.Service{application.NewService(ns)}
}

// start asks for permission once and routes a notification's click to the
// stack and instance it was about.
func (n *notifier) start(d *desk) {
	if n.ns == nil {
		return
	}
	n.d = d
	go func() {
		if ok, err := n.ns.RequestNotificationAuthorization(); err != nil || !ok {
			fmt.Fprintln(os.Stderr, "desk: notifications not authorised")
		}
	}()
	n.ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil || r.Response.UserInfo == nil {
			return
		}
		dir, _ := r.Response.UserInfo["dir"].(string)
		inst, _ := r.Response.UserInfo["instance"].(string)
		if dir == "" {
			return
		}
		if r.Response.ActionIdentifier == "fix" {
			// The fix button: pressed here, when the item still needs it;
			// one that asks first opens its page instead.
			// Only the fix the notification named: a port's holder may have
			// changed since, and a different pid is not what was offered.
			id, _ := r.Response.UserInfo["item"].(string)
			named := 0
			switch v := r.Response.UserInfo["pid"].(type) {
			case float64:
				named = int(v)
			case int:
				named = v
			}
			if it, f, ok := firstFix(d.svc.LastAttention(), id); ok && (f.Kind != "holder" || f.PID == named) {
				if err := d.svc.FixItem(it, f); err == nil {
					return
				}
			}
		}
		d.svc.OpenAt(dir, inst)
		d.open(dir)
	})
}

// send says one note; outside a bundle it goes to stderr, so a dev run
// still shows what would have been said.
func (n *notifier) send(note notify.Note) {
	if n.ns == nil {
		fmt.Fprintf(os.Stderr, "desk: would notify: %s — %s (%s)\n", note.Title, note.Body, note.Sub)
		return
	}
	body := note.Body
	if note.Sub != "" {
		body += "\n" + note.Sub
	}
	opts := notifications.NotificationOptions{
		ID:       note.ID,
		Title:    note.Title,
		Body:     body,
		ThreadID: note.Key,
		Data:     map[string]any{"dir": note.Dir, "instance": note.Instance},
	}
	// The item's first fix rides along as a button beside Show (D19), when
	// it is one the menu bar could press too.
	var err error
	id := "instance:" + note.Key + "/" + note.Instance
	if n.d != nil {
		if _, f, ok := firstFix(n.d.svc.LastAttention(), id); ok {
			// One category per label, not per notification, so they do not pile up.
			cat := "doze.fix." + fmt.Sprintf("%x", fnv32(f.Label))
			if err = n.ns.RegisterNotificationCategory(notifications.NotificationCategory{ID: cat, Actions: []notifications.NotificationAction{{ID: "fix", Title: f.Label}}}); err == nil {
				opts.CategoryID = cat
				opts.Data["item"] = id
				opts.Data["pid"] = f.PID
				err = n.ns.SendNotificationWithActions(opts)
				if err == nil {
					return
				}
			}
		}
	}
	err = n.ns.SendNotification(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "desk: notify:", err)
	}
}

// firstFix is an item's first fix that can be pressed outside the window.
func firstFix(a view.Attention, id string) (view.Item, view.Fix, bool) {
	for _, it := range a.Items {
		if it.ID != id || it.Fixed {
			continue
		}
		for _, f := range it.Fixes {
			switch {
			case f.Kind == "holder", f.Kind == "do" && (f.Verb == "restart" || f.Verb == "task"):
				return it, f, true
			}
		}
	}
	return view.Item{}, view.Fix{}, false
}

func fnv32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
