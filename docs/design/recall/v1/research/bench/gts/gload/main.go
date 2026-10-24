package main

import (
	"fmt"
	"runtime"

	"github.com/odvcencio/gotreesitter/grammars"
)

func rss() (float64, float64) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20), float64(m.Sys) / (1 << 20)
}

func main() {
	h, s := rss()
	fmt.Printf("start heap=%.1fMB sys=%.1fMB\n", h, s)
	grammars.GoLanguage()
	h, s = rss()
	fmt.Printf("after Go grammar heap=%.1fMB sys=%.1fMB\n", h, s)
	grammars.TypescriptLanguage(); grammars.TsxLanguage(); grammars.JavascriptLanguage(); grammars.PythonLanguage()
	h, s = rss()
	fmt.Printf("after go+ts+tsx+js+py heap=%.1fMB sys=%.1fMB\n", h, s)
}
