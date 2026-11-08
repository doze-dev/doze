//go:build windows

// Package dllhook prints the Go stack behind every DLL loaded after it starts:
// imported by the probe (-tags dllhook), it says which code loads what before
// main. Go initialises packages in import-path order once their dependencies
// are done, so this one (github.com/doze-dev/...) runs before Wails' own.
package dllhook

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

// loadedData is LDR_DLL_LOADED_NOTIFICATION_DATA.
type loadedData struct {
	Flags       uint32
	FullDllName *unicodeString
	BaseDllName *unicodeString
	DllBase     uintptr
	SizeOfImage uint32
}

var cookie uintptr

func init() {
	reg := windows.NewLazySystemDLL("ntdll.dll").NewProc("LdrRegisterDllNotification")
	r, _, _ := reg.Call(0, windows.NewCallback(loaded), 0, uintptr(unsafe.Pointer(&cookie)))
	fmt.Fprintf(os.Stderr, "dllhook: registered (status 0x%x)\n", r)
}

func loaded(reason uint32, data *loadedData, _ uintptr) uintptr {
	if reason != 1 || data == nil || data.BaseDllName == nil {
		return 0
	}
	name := windows.UTF16ToString(unsafe.Slice(data.BaseDllName.Buffer, data.BaseDllName.Length/2))
	buf := make([]byte, 16<<10)
	buf = buf[:runtime.Stack(buf, false)]
	var frames []string
	for _, l := range strings.Split(string(buf), "\n") {
		if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "goroutine") {
			continue
		}
		if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, "dllhook") && !strings.HasPrefix(l, "runtime.") && !strings.HasPrefix(l, "syscall.") && !strings.HasPrefix(l, "golang.org/x/sys/windows.") {
			frames = append(frames, l)
		}
	}
	if len(frames) > 6 {
		frames = frames[:6]
	}
	fmt.Fprintf(os.Stderr, "dllhook: %s <- %s\n", strings.ToLower(name), strings.Join(frames, " <- "))
	return 0
}
