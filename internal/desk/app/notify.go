//go:build purego

package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/doze-dev/doze/internal/desk/notify"
)

// Notifications outside the window, through the system's own (D8). macOS
// delivers them only for an app in a bundle, so a bare binary (the desk
// bench, a dev build run from the shell) logs them to stderr instead.

// inBundle says this binary runs from inside a .app, where the system
// notification centre will take it.
func inBundle() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(exe, ".app/Contents/MacOS/")
}

// notifier is the service that sends, when there is one.
type notifier struct {
	ns *notifications.NotificationService
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
	err := n.ns.SendNotification(notifications.NotificationOptions{
		ID:       note.ID,
		Title:    note.Title,
		Body:     body,
		ThreadID: note.Key,
		Data:     map[string]any{"dir": note.Dir, "instance": note.Instance},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "desk: notify:", err)
	}
}
