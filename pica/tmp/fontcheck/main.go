package main

import (
	"fmt"
	"os"

	"github.com/pavlos/typeset/pdf/ttf"
)

func main() {
	data, err := os.ReadFile("/Users/pavloschristforou/repos/typeset/pdf/fonts/FiraMono-Regular.ttf")
	if err != nil {
		panic(err)
	}
	f, err := ttf.Parse(data)
	if err != nil {
		panic(err)
	}
	check := func(name string, runes []rune) {
		miss := 0
		for _, r := range runes {
			if _, ok := f.CharToGID[int(r)]; !ok {
				miss++
			}
		}
		fmt.Printf("%-28s %d/%d covered\n", name, len(runes)-miss, len(runes))
	}
	check("half blocks (▀▄█)", []rune{0x2580, 0x2584, 0x2588})
	q := []rune{}
	for r := rune(0x2596); r <= 0x259F; r++ {
		q = append(q, r)
	}
	check("quadrants (U+2596..259F)", q)
	b := []rune{}
	for r := rune(0x2800); r <= 0x28FF; r += 17 {
		b = append(b, r)
	}
	check("braille (U+2800.. sample)", b)
}
