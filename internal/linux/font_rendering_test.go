//go:build linux && (amd64 || arm64)

package linux

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The desktop's settings say rgb subpixels (Xft.rgba=rgb) with antialiasing
// on. Text must still be grayscale: LCD glyphs have colored fringes that
// read as the wrong color on dark and light text alike.
func TestFontRenderingIgnoresLCDOrder(t *testing.T) {
	got := fontRenderingFrom(1, 1, "hintslight")
	want := platform.FontRendering{Antialias: "gray", Hinting: "slight"}
	if got != want {
		t.Fatalf("fontRenderingFrom(rgb desktop) = %+v, want %+v", got, want)
	}
	if got.Subpixels != "" {
		t.Fatalf("Subpixels = %q, want none", got.Subpixels)
	}
}

func TestFontRenderingMapping(t *testing.T) {
	for _, c := range []struct {
		name               string
		antialias, hinting int32
		hintStyle          string
		want               platform.FontRendering
	}{
		{"antialias off", 0, 1, "hintslight", platform.FontRendering{Antialias: "none", Hinting: "slight"}},
		{"hinting off", 1, 0, "hintfull", platform.FontRendering{Antialias: "gray", Hinting: "none"}},
		{"unknown style", 1, 1, "hintbogus", platform.FontRendering{Antialias: "gray"}},
		{"unset", -1, -1, "", platform.FontRendering{}},
	} {
		if got := fontRenderingFrom(c.antialias, c.hinting, c.hintStyle); got != c.want {
			t.Errorf("%s: fontRenderingFrom = %+v, want %+v", c.name, got, c.want)
		}
	}
}
