//go:build purego

package app

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// docsURL is the app's page in the docs, for Help.
const docsURL = "https://doze-dev.com/docs/doze/app/library/"

// appMenu is doze's own menu bar, set in place of the framework's default
// (whose Help loaded the framework's site into the window). Items that
// move the page are passed to it (serve.EventMenu); the window is opened
// first if it is closed, so a menu item always lands somewhere.
func (d *desk) appMenu() *application.Menu {
	m := d.app.NewMenu()
	page := func(item string) func(*application.Context) {
		return func(*application.Context) {
			d.open("")
			d.svc.Menu(item)
		}
	}

	if runtime.GOOS == "darwin" {
		app := m.AddSubmenu("doze")
		app.AddRole(application.About)
		app.Add("Check for Updates…").OnClick(func(*application.Context) {
			d.svc.CheckForUpdates()
			d.open("")
		})
		app.AddSeparator()
		app.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) {
			d.svc.OpenSettings()
			d.open("")
		})
		app.AddSeparator()
		app.AddRole(application.ServicesMenu)
		app.AddSeparator()
		app.AddRole(application.Hide)
		app.AddRole(application.HideOthers)
		app.AddRole(application.UnHide)
		app.AddSeparator()
		app.Add("Quit doze").SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) { d.app.Quit() })
	}

	file := m.AddSubmenu("File")
	file.Add("Open Folder…").SetAccelerator("CmdOrCtrl+O").OnClick(page("open"))
	file.AddSeparator()
	file.AddRole(application.CloseWindow)

	m.AddRole(application.EditMenu)

	goMenu := m.AddSubmenu("Go")
	goMenu.Add("Home").SetAccelerator("CmdOrCtrl+0").OnClick(page("library"))
	goMenu.Add("Back").SetAccelerator("CmdOrCtrl+[").OnClick(page("back"))
	goMenu.Add("Forward").SetAccelerator("CmdOrCtrl+]").OnClick(page("forward"))
	goMenu.AddSeparator()
	goMenu.Add("All Logs of the Stack").SetAccelerator("CmdOrCtrl+L").OnClick(page("logs"))
	goMenu.Add("Search or Do…").SetAccelerator("CmdOrCtrl+K").OnClick(page("palette"))
	goMenu.Add("Needs You").SetAccelerator("CmdOrCtrl+Shift+N").OnClick(page("bell"))

	view := m.AddSubmenu("View")
	view.AddRole(application.ToggleFullscreen)

	m.AddRole(application.WindowMenu)

	help := m.AddSubmenu("Help")
	help.Add("Keyboard Shortcuts").SetAccelerator("CmdOrCtrl+/").OnClick(page("help"))
	help.Add("doze Docs").OnClick(func(*application.Context) {
		_ = d.app.Browser.OpenURL(docsURL)
	})
	return m
}
