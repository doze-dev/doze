// win opens one Wails window, waits for its page to report it rendered, keeps
// it open a few seconds so its memory can be sampled, and quits: proof that
// the GUI libraries, loaded lazily or not, drive a real window.
package main

import (
	"embed"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed index.html
var assets embed.FS

func main() {
	start := time.Now()
	app := application.New(application.Options{
		Name:   "d0win",
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
	})
	app.Event.On("rendered", func(e *application.CustomEvent) {
		fmt.Printf("rendered after %s: %v\n", time.Since(start).Round(time.Millisecond), e.Data)
		go func() {
			time.Sleep(5 * time.Second)
			app.Quit()
		}()
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "d0", Width: 800, Height: 600, URL: "/"})
	go func() {
		time.Sleep(60 * time.Second)
		fmt.Println("timeout: the page never rendered")
		os.Exit(2)
	}()
	if err := app.Run(); err != nil {
		fmt.Println("run:", err)
		os.Exit(1)
	}
}
