//go:build windows && wails

// smoke opens a real Wails window and quits once its page has loaded, after
// an InvokeSync from another goroutine has reached the main thread. It exits
// 0 on success and non-zero on a failure or after 90 seconds. With the
// argument "worker" the App is built on another goroutine and run on the
// startup thread, as an app that builds it elsewhere would.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	worker := len(os.Args) > 1 && os.Args[1] == "worker"
	go func() {
		time.Sleep(90 * time.Second)
		fmt.Println("smoke: no page after 90 s")
		os.Exit(3)
	}()

	var app *application.App
	build := func() { app = application.New(application.Options{Name: "wailswin-smoke"}) }
	if worker {
		done := make(chan struct{})
		go func() { build(); close(done) }()
		<-done
	} else {
		build()
	}

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "wailswin smoke",
		HTML:   "<html><body><h1>ok</h1></body></html>",
		Width:  400,
		Height: 300,
	})
	start := time.Now()
	win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		go func() {
			ran := false
			application.InvokeSync(func() { ran = true })
			fmt.Printf("smoke: page loaded after %s; InvokeSync from a goroutine ran: %v\n", time.Since(start).Round(time.Millisecond), ran)
			if !ran {
				os.Exit(4)
			}
			app.Quit()
		}()
	})
	if err := app.Run(); err != nil {
		fmt.Println("smoke: run:", err)
		os.Exit(1)
	}
	fmt.Println("smoke: ok")
}
