package ttf

import (
	"os"
	"testing"
)

func loadFont(t *testing.T, name string) *TTFont {
	t.Helper()
	raw, err := os.ReadFile("../fonts/" + name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

// Expected values verified against an independent GPOS reader; Fira
// Sans has unitsPerEm 1000, so font units equal PDF units.
func TestKernFiraSans(t *testing.T) {
	for _, name := range []string{"FiraSans-Regular.ttf", "FiraSans-Bold.ttf"} {
		f := loadFont(t, name)
		cases := []struct {
			a, b rune
			want int
		}{
			{'A', 'V', -45},
			{'V', 'A', -45},
			{'T', 'o', -80},
			{'W', 'a', -30},
			{'L', 'Y', -100},
			{'F', '.', -80},
			{'P', ',', -130},
			{'T', 'y', -60},
			{'a', 'b', 0},
			{'f', 'f', 0},
		}
		for _, c := range cases {
			if got := f.Kern(c.a, c.b); got != c.want {
				t.Errorf("%s: Kern(%q, %q) = %d, want %d", name, c.a, c.b, got, c.want)
			}
			// Second call exercises the cache.
			if got := f.Kern(c.a, c.b); got != c.want {
				t.Errorf("%s: cached Kern(%q, %q) = %d, want %d", name, c.a, c.b, got, c.want)
			}
		}
	}
}

func TestKernFiraMonoIsZero(t *testing.T) {
	f := loadFiraMono(t)
	for _, pair := range [][2]rune{{'A', 'V'}, {'T', 'o'}, {'L', 'Y'}} {
		if got := f.Kern(pair[0], pair[1]); got != 0 {
			t.Errorf("Kern(%q, %q) = %d, want 0 for monospace", pair[0], pair[1], got)
		}
	}
}
