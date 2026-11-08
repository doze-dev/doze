//go:build wails && (windows || linux)

// smoke opens a real Wails window and quits once its page has loaded, after
// an InvokeSync from another goroutine has reached the main thread. It exits
// 0 on success and non-zero on a failure or after 90 seconds.
//
//	smoke          the App is built and run on the startup thread
//	smoke worker   the App is built on another goroutine, run on the startup thread
//	smoke service  as worker, and the window is opened by a service's
//	               ServiceStartup, which Run calls before its message loop
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	app   *application.App
	start = time.Now()
)

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	go func() {
		time.Sleep(90 * time.Second)
		fmt.Println("smoke: no page after 90 s")
		os.Exit(3)
	}()

	opts := application.Options{Name: "wailswin-smoke"}
	if mode == "service" {
		opts.Services = []application.Service{application.NewService(&opener{})}
	}
	if mode == "worker" || mode == "service" {
		done := make(chan struct{})
		go func() { app = application.New(opts); close(done) }()
		<-done
	} else {
		app = application.New(opts)
	}
	if mode != "service" {
		openWindow()
	}
	if err := app.Run(); err != nil {
		fmt.Println("smoke: run:", err)
		os.Exit(1)
	}
	fmt.Println("smoke: ok")
}

// opener opens the window from ServiceStartup.
type opener struct{}

func (*opener) ServiceStartup(context.Context, application.ServiceOptions) error {
	openWindow()
	return nil
}

func openWindow() {
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "wailswin smoke",
		HTML:   "<html><body><h1>ok</h1></body></html>",
		Width:  400,
		Height: 300,
	})
	win.Show()
	win.OnWindowEvent(pageLoaded, func(*application.WindowEvent) {
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
}
