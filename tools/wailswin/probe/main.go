//go:build windows

// probe is the program measured: it prints "ok" and exits. Built plain and
// with Wails linked (-tags wails), never opening a window, it shows what
// linking Wails costs a Windows process that is a CLI or a daemon.
//
// With WAILSWIN_REPORT=1 it reports on itself instead: its peak memory, the
// DLLs loaded by the time main runs, and whether its main thread is already
// in a COM apartment (Wails' webview package initialises COM in init).
package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func main() {
	if os.Getenv("WAILSWIN_REPORT") == "" {
		fmt.Println("ok")
		return
	}
	runtime.LockOSThread()
	// The modules first, before this report loads anything of its own.
	mods := modules()
	fmt.Printf("modules %d\n", len(mods))
	fmt.Printf("module-list %s\n", strings.Join(mods, ","))
	fmt.Printf("peak-working-set %d\n", peakWorkingSet())
	fmt.Printf("com %s\n", apartment())
}

func modules() []string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var me windows.ModuleEntry32
	me.Size = uint32(unsafe.Sizeof(me))
	var out []string
	for err := windows.Module32First(snap, &me); err == nil; err = windows.Module32Next(snap, &me) {
		out = append(out, strings.ToLower(windows.UTF16ToString(me.Module[:])))
	}
	sort.Strings(out)
	return out
}

// processMemoryCounters is PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func peakWorkingSet() uintptr {
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")
	if r, _, _ := proc.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); r == 0 {
		return 0
	}
	return c.PeakWorkingSetSize
}

// apartment asks for a multithreaded apartment: S_OK means none was set up,
// RPC_E_CHANGED_MODE means something already made this a single-threaded one.
func apartment() string {
	co := windows.NewLazySystemDLL("ole32.dll").NewProc("CoInitializeEx")
	r, _, _ := co.Call(0, 0 /* COINIT_MULTITHREADED */)
	switch uint32(r) {
	case 0:
		return "none (fresh)"
	case 1:
		return "already multithreaded"
	case 0x80010106:
		return "single-threaded, set up before main"
	}
	return fmt.Sprintf("hresult 0x%08x", uint32(r))
}
