// fsev: FSEvents from Go with CGO_ENABLED=0, via purego.
//
//	fsev <root> <sinceEventId|now> <seconds>
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	cfStringCreateWithCString func(alloc uintptr, s string, enc uint32) uintptr
	cfArrayCreate             func(alloc uintptr, vals *uintptr, n int, cb uintptr) uintptr
	fsEventStreamCreate       func(alloc uintptr, cb uintptr, ctx uintptr, paths uintptr, since uint64, latency float64, flags uint32) uintptr
	fsEventStreamSetDQ        func(stream uintptr, q uintptr)
	fsEventStreamStart        func(stream uintptr) bool
	fsEventStreamStop         func(stream uintptr)
	fsEventStreamInvalidate   func(stream uintptr)
	fsEventStreamRelease      func(stream uintptr)
	fsEventsGetCurrentEventId func() uint64
	dispatchQueueCreate       func(label string, attr uintptr) uintptr
)

const (
	flagNoDefer    = 0x02
	flagFileEvents = 0x10
	evHistoryDone  = 0x10 // kFSEventStreamEventFlagHistoryDone
)

var events, historyEvents atomic.Int64
var start time.Time
var verbose = os.Getenv("V") != ""
var historyDone atomic.Bool

func cstr(p uintptr) string {
	var b []byte
	for {
		c := *(*byte)(unsafe.Pointer(p))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
		p++
	}
}

func main() {
	root := os.Args[1]
	since := uint64(0xFFFFFFFFFFFFFFFF) // kFSEventStreamEventIdSinceNow
	if os.Args[2] != "now" {
		since, _ = strconv.ParseUint(os.Args[2], 10, 64)
	}
	secs, _ := strconv.Atoi(os.Args[3])

	cs, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	sys, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	must(err)
	purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfArrayCreate, cf, "CFArrayCreate")
	purego.RegisterLibFunc(&fsEventStreamCreate, cs, "FSEventStreamCreate")
	purego.RegisterLibFunc(&fsEventStreamSetDQ, cs, "FSEventStreamSetDispatchQueue")
	purego.RegisterLibFunc(&fsEventStreamStart, cs, "FSEventStreamStart")
	purego.RegisterLibFunc(&fsEventStreamStop, cs, "FSEventStreamStop")
	purego.RegisterLibFunc(&fsEventStreamInvalidate, cs, "FSEventStreamInvalidate")
	purego.RegisterLibFunc(&fsEventStreamRelease, cs, "FSEventStreamRelease")
	purego.RegisterLibFunc(&fsEventsGetCurrentEventId, cs, "FSEventsGetCurrentEventId")
	purego.RegisterLibFunc(&dispatchQueueCreate, sys, "dispatch_queue_create")
	arrayCB, err := purego.Dlsym(cf, "kCFTypeArrayCallBacks")
	must(err)

	cb := purego.NewCallback(func(stream, info, n, paths, flags, ids uintptr) uintptr {
		now := time.Since(start)
		for i := uintptr(0); i < n; i++ {
			p := cstr(*(*uintptr)(unsafe.Pointer(paths + i*8)))
			fl := *(*uint32)(unsafe.Pointer(flags + i*4))
			id := *(*uint64)(unsafe.Pointer(ids + i*8))
			if fl&evHistoryDone != 0 {
				historyDone.Store(true)
				fmt.Printf("history done at %v after %d replayed events\n", now.Round(time.Millisecond), historyEvents.Load())
				continue
			}
			if !historyDone.Load() && since != 0xFFFFFFFFFFFFFFFF {
				historyEvents.Add(1)
			}
			events.Add(1)
			if verbose {
				fmt.Printf("%v id=%d flags=%#x %s\n", now.Round(time.Millisecond), id, fl, p)
			}
		}
		return 0
	})

	s := cfStringCreateWithCString(0, root+"\x00", 0x08000100) // kCFStringEncodingUTF8
	arr := cfArrayCreate(0, &s, 1, arrayCB)
	start = time.Now()
	st := fsEventStreamCreate(0, cb, 0, arr, since, 0.05, flagFileEvents|flagNoDefer)
	if st == 0 {
		panic("FSEventStreamCreate failed")
	}
	fsEventStreamSetDQ(st, dispatchQueueCreate("fsev\x00", 0))
	if !fsEventStreamStart(st) {
		panic("start failed")
	}
	fmt.Printf("watching %s since=%d current=%d setup=%v\n", root, since, fsEventsGetCurrentEventId(), time.Since(start).Round(time.Microsecond))
	time.Sleep(time.Duration(secs) * time.Second)
	fsEventStreamStop(st)
	fsEventStreamInvalidate(st)
	fsEventStreamRelease(st)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("events=%d lastEventId=%d goSys=%dMB\n", events.Load(), fsEventsGetCurrentEventId(), m.Sys>>20)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
