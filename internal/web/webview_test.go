//go:build js

package web

import (
	"syscall/js"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// The web views are platform web views, and a surface keeps them in a set. The
// assertions are here, not in the package: a conversion in the package would
// link the methods into every app.
var (
	_ platform.WebView = (*webView)(nil)
	_ webViewSet       = (*webViews)(nil)
)

// TestVisibleRects: the visible parts of a clip are rectangles that do not
// overlap, lie in the clip, and take no part of a cover. Node runs these tests,
// with no browser.
func TestVisibleRects(t *testing.T) {
	square := platform.RectF{W: 100, H: 100}
	for _, tc := range []struct {
		name   string
		clip   platform.RectF
		covers []platform.RectF
		area   float64
	}{
		{"no cover", square, nil, 10000},
		{"cover inside", square, []platform.RectF{{X: 10, Y: 10, W: 20, H: 20}}, 9600},
		// The covers overlap in a 20 by 20 square, which is taken once.
		{"covers overlap", square, []platform.RectF{{W: 60, H: 60}, {X: 40, Y: 40, W: 60, H: 60}}, 3200},
		{"cover outside", square, []platform.RectF{{X: 200, W: 10, H: 10}}, 10000},
		{"cover takes all", square, []platform.RectF{{X: -10, Y: -10, W: 200, H: 200}}, 0},
		// A cover past the clip takes only the part of it the clip has.
		{"clip not at origin", platform.RectF{X: 50, Y: 50, W: 100, H: 100}, []platform.RectF{{W: 100, H: 100}}, 7500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rects := visibleRects(tc.clip, tc.covers)
			var area float64
			for i, r := range rects {
				area += r.W * r.H
				if r.X < tc.clip.X || r.Y < tc.clip.Y || r.X+r.W > tc.clip.X+tc.clip.W || r.Y+r.H > tc.clip.Y+tc.clip.H {
					t.Errorf("rect %v leaves the clip %v", r, tc.clip)
				}
				if coveredBy(tc.covers, r.X+r.W/2, r.Y+r.H/2) {
					t.Errorf("rect %v is under a cover", r)
				}
				for _, o := range rects[i+1:] {
					if rectsOverlap(r, o) {
						t.Errorf("rects %v and %v overlap", r, o)
					}
				}
			}
			if area != tc.area {
				t.Errorf("visible area %v, want %v (rects %v)", area, tc.area, rects)
			}
		})
	}
}

// TestClipPathCSS: the clip-path is relative to the frame's corner, which is the
// iframe's, and is "none" when the whole frame shows.
func TestClipPathCSS(t *testing.T) {
	frame := platform.RectF{X: 10, Y: 20, W: 100, H: 50}
	for _, tc := range []struct {
		name   string
		clip   platform.RectF
		covers []platform.RectF
		want   string
	}{
		{"whole frame", frame, nil, "none"},
		{"no clip", platform.RectF{X: 10, Y: 20}, nil, ""},
		{"all covered", frame, []platform.RectF{frame}, ""},
		// The left half is covered, so the right half shows.
		{"left half covered", frame, []platform.RectF{{X: 10, Y: 20, W: 50, H: 50}}, `path("M50 0h50v50h-50z ")`},
		// A clip smaller than the frame, as a scrolled container gives, shows its part.
		{"scrolled", platform.RectF{X: 10, Y: 20, W: 100, H: 30}, nil, `path("M0 0h100v30h-100z ")`},
	} {
		if got := clipPathCSS(frame, tc.clip, tc.covers); got != tc.want {
			t.Errorf("%s: clipPathCSS = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestWebViewPlacement shows web views over a surface where the placements say:
// boxes, clips and stacking follow the placements, a view the placements leave
// out is hidden, and an untrusted view gets the sandbox that keeps it out of the
// app's origin.
func TestWebViewPlacement(t *testing.T) {
	doc := installFakeDocument(t)
	s := &surface{w: &window{el: doc.Call("createElement", "div")}, scale: 1}
	trusted, err := s.NewWebView(&platform.WindowOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	untrusted, err := s.NewWebView(&platform.WindowOptions{Untrusted: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b := trusted.(*webView), untrusted.(*webView)
	if got := b.el.Call("getAttribute", "sandbox"); got.String() != "allow-scripts" {
		t.Errorf("untrusted sandbox = %v, want allow-scripts", got)
	}
	if got := a.el.Call("getAttribute", "sandbox"); !got.IsNull() {
		t.Errorf("trusted view has sandbox %v", got)
	}

	full := platform.RectF{X: 10, Y: 20, W: 100, H: 50}
	right := platform.RectF{X: 60, Y: 20, W: 100, H: 50}
	// b is painted after a, so it stacks above it.
	s.PlaceWebViews([]platform.WebViewPlacement{
		{WebView: a, Frame: full, Clip: full},
		{WebView: b, Frame: right, Clip: right},
	})
	wantStyle(t, "a", a.el, map[string]string{"display": "block", "left": "10px", "top": "20px", "width": "100px", "height": "50px", "zIndex": "1", "clipPath": "none"})
	wantStyle(t, "b", b.el, map[string]string{"display": "block", "left": "60px", "top": "20px", "zIndex": "2", "clipPath": "none"})

	// A frame painted over the left half of a takes that half, and b is left out
	// of the placements: b hides, and a keeps its page.
	cover := platform.RectF{X: 10, Y: 20, W: 50, H: 50}
	s.PlaceWebViews([]platform.WebViewPlacement{{WebView: a, Frame: full, Clip: full, Covers: []platform.RectF{cover}}})
	wantStyle(t, "a", a.el, map[string]string{"display": "block", "zIndex": "1", "clipPath": `path("M50 0h50v50h-50z ")`})
	wantStyle(t, "b", b.el, map[string]string{"display": "none"})

	// b takes the keyboard while it shows; left out of the placements, it hides
	// and the keyboard goes back to the content's input.
	s.input = doc.Call("createElement", "textarea")
	s.PlaceWebViews([]platform.WebViewPlacement{
		{WebView: a, Frame: full, Clip: full, Covers: []platform.RectF{cover}},
		{WebView: b, Frame: right, Clip: right},
	})
	doc.Set("activeElement", b.el)
	s.PlaceWebViews([]platform.WebViewPlacement{{WebView: a, Frame: full, Clip: full, Covers: []platform.RectF{cover}}})
	wantStyle(t, "b", b.el, map[string]string{"display": "none"})
	if !s.input.Get("focused").Bool() {
		t.Error("a hidden web view kept the keyboard")
	}

	a.Close()
	a.Close() // a second close does nothing
	if !a.el.Get("removed").Bool() {
		t.Error("a closed view is still in the document")
	}
	set := s.views.(*webViews)
	if len(set.list) != 1 || set.list[0] != b {
		t.Errorf("views after closing a: %d, want only b", len(set.list))
	}
	// A closed view is not shown again.
	s.PlaceWebViews([]platform.WebViewPlacement{{WebView: a, Frame: full, Clip: full}})
	wantStyle(t, "a", a.el, map[string]string{"zIndex": "1", "clipPath": `path("M50 0h50v50h-50z ")`})

	s.views.closeAll()
	if len(set.list) != 0 || !b.el.Get("removed").Bool() {
		t.Errorf("closeAll left %d views, b removed %v", len(set.list), b.el.Get("removed").Bool())
	}
}

func rectsOverlap(a, b platform.RectF) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// wantStyle checks the inline style of an element, property by property.
func wantStyle(t *testing.T, name string, el js.Value, want map[string]string) {
	t.Helper()
	for prop, v := range want {
		if got := el.Get("style").Get(prop).String(); got != v {
			t.Errorf("%s: style.%s = %q, want %q", name, prop, got, v)
		}
	}
}

// fakeDocumentSource is a document of plain objects, for the browser's own,
// which Node lacks: its elements keep the styles and attributes set on them.
const fakeDocumentSource = `(() => {
	const element = () => ({
		style: {},
		attributes: {},
		removed: false,
		setAttribute(name, value) { this.attributes[name] = String(value); },
		getAttribute(name) { return name in this.attributes ? this.attributes[name] : null; },
		appendChild() {},
		addEventListener() {},
		removeEventListener() {},
		focus() { this.focused = true; },
		remove() { this.removed = true; },
	});
	return { createElement: element, activeElement: null };
})()`

// installFakeDocument puts the fake document in place for the rest of the test.
func installFakeDocument(t *testing.T) js.Value {
	t.Helper()
	global := js.Global()
	old := global.Get("document")
	doc := global.Call("eval", fakeDocumentSource)
	global.Set("document", doc)
	t.Cleanup(func() {
		if old.IsUndefined() {
			global.Delete("document")
		} else {
			global.Set("document", old)
		}
	})
	return doc
}
