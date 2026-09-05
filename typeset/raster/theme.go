package raster

import (
	"fmt"
	"strings"
)

// A Theme is what the palette's eight entries look like in a
// renderer that has colours to choose: the foreground and background
// colour of each ink index, the page's ground, the panel's ground and
// its rule, as CSS colours. Index 0 is the default: the theme's own
// text colour and ground. The format states no colours (RASTER.t,
// "Ink"); a theme is a renderer's, and an app picks one.
type Theme struct {
	Name         string
	FG, BG       [8]string
	Ground, Rule string // the page's ground and the panel's border
	Panel        string // the panel's ground
}

// Themes are the built-in themes by name: "teletext", the seven hues
// of teletext on a night ground; "cellimage", the sixteen-colour
// image palette of the cellimage converter mapped onto the seven
// roles by luminance, on black; "solarized" and "solarized-light",
// Ethan Schoonover's palette on its dark and light grounds.
var Themes = map[string]Theme{
	"teletext":        Teletext,
	"cellimage":       Cellimage,
	"solarized":       Solarized,
	"solarized-light": SolarizedLight,
}

var (
	Teletext = Theme{
		Name:   "teletext",
		FG:     [8]string{"#cfd6e4", "#ec4b3c", "#3fd06f", "#f2c53d", "#4b7ff0", "#d55cd8", "#3fc9e6", "#f4f6fa"},
		BG:     [8]string{"#05080f", "#b3271b", "#1f8a44", "#b98e12", "#1f4fc4", "#9a2f9d", "#1c8fa8", "#e6e9f0"},
		Ground: "#0a0e17", Panel: "#05080f", Rule: "#1f2a3f",
	}
	// The cellimage palette (research/cellimage-converter-spec.md §1,
	// frozen there): the light member of each hue pair for text, the
	// dark member for grounds; grey-light for default text, white for
	// white, black for the ground, grey-dark for the rule.
	Cellimage = Theme{
		Name:   "cellimage",
		FG:     [8]string{"#A0A0A0", "#E04A2E", "#4CBE52", "#EDD94F", "#55A8E6", "#EE82B0", "#2E8C7E", "#FFFFFF"},
		BG:     [8]string{"#000000", "#7A1E1E", "#1E5C2A", "#8A5A2B", "#1E4C8C", "#8C4E9E", "#2E8C7E", "#A0A0A0"},
		Ground: "#000000", Panel: "#000000", Rule: "#4A4A4A",
	}
	// Solarized (Ethan Schoonover, 2011): base0 on base03, the eight
	// accents by their names, white as base2; grounds are the accents
	// themselves, on which the theme's light text reads.
	Solarized = Theme{
		Name:   "solarized",
		FG:     [8]string{"#839496", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#eee8d5"},
		BG:     [8]string{"#002b36", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#eee8d5"},
		Ground: "#002b36", Panel: "#002b36", Rule: "#586e75",
	}
	// Solarized light: base00 on base3, the same accents.
	SolarizedLight = Theme{
		Name:   "solarized-light",
		FG:     [8]string{"#657b83", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#fdf6e3"},
		BG:     [8]string{"#fdf6e3", "#dc322f", "#859900", "#b58900", "#268bd2", "#d33682", "#2aa198", "#073642"},
		Ground: "#fdf6e3", Panel: "#fdf6e3", Rule: "#93a1a1",
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
