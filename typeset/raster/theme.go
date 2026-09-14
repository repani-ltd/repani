package raster

import (
	"fmt"
	"strings"
)

// A Theme is what the palette's eight entries look like in a
// renderer that has colours to choose: the foreground and background
// colour of each ink index, the document's ground, a raster's ground
// and its border, as CSS colours. Index 0 is the default: the theme's
// own text colour and ground. The format states no colours (RASTER.t,
// "Ink"); a theme is a renderer's, and an app picks one.
type Theme struct {
	Name         string
	FG, BG       [8]string
	Ground, Rule string // the document's ground and a raster's border
	Panel        string // a raster's ground (the --panel variable)
}

// Themes are the built-in themes by name: "teletext", the seven hues
// of teletext on a night ground, and "teletext-light", the same dark
// on light.
var Themes = map[string]Theme{
	"teletext":       Teletext,
	"teletext-light": TeletextLight,
}

var (
	Teletext = Theme{
		Name:   "teletext",
		FG:     [8]string{"#cfd6e4", "#ec4b3c", "#3fd06f", "#f2c53d", "#4b7ff0", "#d55cd8", "#3fc9e6", "#f4f6fa"},
		BG:     [8]string{"#05080f", "#b3271b", "#1f8a44", "#b98e12", "#1f4fc4", "#9a2f9d", "#1c8fa8", "#e6e9f0"},
		Ground: "#0a0e17", Panel: "#05080f", Rule: "#1f2a3f",
	}
	// Teletext, dark on light: the ground is the dark theme's white,
	// the text a dark navy; the text hues are the dark theme's bar
	// grounds, which carry contrast on white; the bars stay those
	// same saturated hues, with white text on them as on the dark
	// theme; and index 7, light text on a bar, is white as text and a
	// dark slate as a band, the inverse of the dark theme's white band.
	TeletextLight = Theme{
		Name:   "teletext-light",
		FG:     [8]string{"#1c2333", "#b3271b", "#1f8a44", "#8a6a0b", "#1f4fc4", "#9a2f9d", "#1c7f98", "#ffffff"},
		BG:     [8]string{"#ffffff", "#b3271b", "#1f8a44", "#b98e12", "#1f4fc4", "#9a2f9d", "#1c8fa8", "#2b3140"},
		Ground: "#f4f6fa", Panel: "#ffffff", Rule: "#c9d1de",
	}
)

// CSS returns the theme as a stylesheet fragment: the colours as
// variables on :root, and the ink classes the HTML renderer emits,
// fN for a foreground and bN for a background. Text on background 7,
// the theme's white, takes the ground's colour so it reads.
func (t Theme) CSS() string {
	var b strings.Builder
	fmt.Fprintf(&b, ":root {\n  --ground: %s; --panel: %s; --rule: %s;\n", t.Ground, t.Panel, t.Rule)
	for i := range 8 {
		fmt.Fprintf(&b, "  --c%d: %s; --g%d: %s;\n", i, t.FG[i], i, t.BG[i])
	}
	b.WriteString("}\n")
	for i := 1; i < 8; i++ {
		fmt.Fprintf(&b, ".f%d { color: var(--c%d) }\n", i, i)
	}
	for i := 1; i < 7; i++ {
		fmt.Fprintf(&b, ".b%d { background: var(--g%d) }\n", i, i)
	}
	b.WriteString(".b7 { background: var(--g7); color: var(--ground) }\n")
	return b.String()
}
