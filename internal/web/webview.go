//go:build js

package web

import (
	"log"
	"slices"
	"strings"
	"syscall/js"

	"github.com/egoist/mygo/internal/platform"
)

// A web view of a surface (Surface.NewWebView) is an iframe above the canvas.
// The content leaves transparent the canvas where a web view shows, and paints
// over the web view what covers it; the iframe is clipped to what the content
// leaves visible, so the pointer there reaches the canvas, and the paint over
// it stays on top.
//
// A page the app does not trust (WindowOptions.Untrusted) gets a sandbox
// without allow-same-origin, so the browser gives it an origin of its own: it
// reaches neither the app's storage and cookies nor its frame, and it asks for
// no permission, clipboard or full screen the sandbox does not grant. The
// browser does not tell the app what such a page does with its pointer, focus
// or history, so the methods that would need that report it, as their docs
// say, or do nothing.
type webView struct {
	s         *surface
	h         platform.WindowHandler
	el        js.Value // the iframe
	untrusted bool
	closed    bool
	shown     bool // displayed: the content showed the view at the last placement
	url       string
	srcdoc    bool // the page is HTML from LoadHTML, not a URL
	loading   bool
	started   bool // the app has asked for a page; a load before that is the browser's own
	zoom      float64
	userAgent string
	funcs     []js.Func

	// What the iframe shows, as last applied: its box, the clip-path, and its
	// stacking order among the web views.
	frame    RectF
	clipPath string
	z        int
}

// RectF is platform.RectF, which the placements are in.
type RectF = platform.RectF

// NewWebView creates the iframe of a web view under the surface, hidden until
// PlaceWebViews shows it. Of opts, the options of a web view apply.
func (s *surface) NewWebView(o *platform.WindowOptions, h platform.WindowHandler) (platform.WebView, error) {
	el := js.Global().Get("document").Call("createElement", "iframe")
	if o.Untrusted {
		el.Call("setAttribute", "sandbox", "allow-scripts")
	}
	st := el.Get("style")
	st.Set("position", "absolute")
	st.Set("display", "none")
	st.Set("border", "0")
	st.Set("margin", "0")
	st.Set("padding", "0")
	s.w.el.Call("appendChild", el)
	v := &webView{s: s, h: h, el: el, untrusted: o.Untrusted, zoom: 1}
	if o.Zoom > 0 {
		v.zoom = o.Zoom
	}
	if o.BackgroundColor != nil && !o.Transparent {
		v.SetBackgroundColor(*o.BackgroundColor)
	}
	v.listen()
	if s.views == nil {
		s.views = new(webViews)
	}
	s.views.(*webViews).add(v)
	return v, nil
}

// webViewSet is what a surface does with its web views. Only NewWebView makes
// them, and the surface reaches their code through this interface alone, whose
// methods take no web view: a web view in an interface's signature would link
// all its methods into every app (AGENTS.md: apps pay only for what they use).
type webViewSet interface {
	place(views []platform.WebViewPlacement)
	closeAll()
}

// webViews are the web views of a surface, in the order they were made.
type webViews struct{ list []*webView }

func (w *webViews) add(v *webView) { w.list = append(w.list, v) }

// drop removes v from the web views.
func (w *webViews) drop(v *webView) {
	w.list = slices.DeleteFunc(w.list, func(x *webView) bool { return x == v })
}

// place shows the web views where the content's last frame shows them, in the
// order it painted them, and hides the others, which keep their pages.
func (w *webViews) place(views []platform.WebViewPlacement) {
	shown := make(map[*webView]bool, len(views))
	z := 1
	for _, p := range views {
		v, ok := p.WebView.(*webView)
		if !ok || v.closed || shown[v] || !slices.Contains(w.list, v) {
			continue
		}
		if v.place(p, z) {
			shown[v] = true
			z++
		}
	}
	for _, v := range slices.Clone(w.list) {
		if !shown[v] {
			v.hide()
		}
	}
}

// closeAll closes the web views, which closes with the surface.
func (w *webViews) closeAll() {
	for _, v := range slices.Clone(w.list) {
		v.Close()
	}
}

// PlaceWebViews places the web views of the surface, if it has any.
func (s *surface) PlaceWebViews(views []platform.WebViewPlacement) {
	if s.views != nil {
		s.views.place(views)
	}
}

// place shows the view where the placement says, stacked at z, and reports
// whether any of it shows.
func (v *webView) place(p platform.WebViewPlacement, z int) bool {
	clip := clipPathCSS(p.Frame, p.Clip, p.Covers)
	if p.Frame.W <= 0 || p.Frame.H <= 0 || clip == "" {
		v.hide()
		return false
	}
	if v.shown && p.Frame == v.frame && clip == v.clipPath && z == v.z {
		return true
	}
	st := v.el.Get("style")
	st.Set("display", "block")
	st.Set("left", px(p.Frame.X))
	st.Set("top", px(p.Frame.Y))
	st.Set("width", px(p.Frame.W))
	st.Set("height", px(p.Frame.H))
	st.Set("zIndex", itoa(z))
	st.Set("clipPath", clip)
	v.shown, v.frame, v.clipPath, v.z = true, p.Frame, clip, z
	return true
}

func (v *webView) hide() {
	if !v.shown {
		return
	}
	v.shown = false
	v.giveUpKeyboard()
	v.el.Get("style").Set("display", "none")
}

// giveUpKeyboard hands the keyboard back to the content if the view has it: a
// hidden or closed view keeps none (WebViewPlacement.Inert).
func (v *webView) giveUpKeyboard() {
	if js.Global().Get("document").Get("activeElement").Equal(v.el) {
		v.s.focus()
	}
}

// clipPathCSS returns the clip-path that leaves visible of the frame the part of
// clip that no cover takes: "none" when all of the frame shows, "" when nothing
// does. It is relative to the frame's corner, which is the iframe's.
func clipPathCSS(frame, clip RectF, covers []RectF) string {
	if clip.W <= 0 || clip.H <= 0 {
		return ""
	}
	if len(covers) == 0 && clip == frame {
		return "none"
	}
	rects := visibleRects(clip, covers)
	if len(rects) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`path("`)
	for _, r := range rects {
		b.WriteString("M" + ftoa(r.X-frame.X) + " " + ftoa(r.Y-frame.Y) + "h" + ftoa(r.W) + "v" + ftoa(r.H) + "h" + ftoa(-r.W) + "z ")
	}
	b.WriteString(`")`)
	return b.String()
}

// visibleRects returns the parts of clip that no cover takes, as rectangles that
// do not overlap. The covers may overlap each other: the cells of their grid
// decide each cell once, so no cover is taken away twice.
func visibleRects(clip RectF, covers []RectF) []RectF {
	xs := []float64{clip.X, clip.X + clip.W}
	ys := []float64{clip.Y, clip.Y + clip.H}
	for _, c := range covers {
		x0, y0 := max(c.X, clip.X), max(c.Y, clip.Y)
		x1, y1 := min(c.X+c.W, clip.X+clip.W), min(c.Y+c.H, clip.Y+clip.H)
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		xs = append(xs, x0, x1)
		ys = append(ys, y0, y1)
	}
	slices.Sort(xs)
	slices.Sort(ys)
	xs, ys = slices.Compact(xs), slices.Compact(ys)
	var out []RectF
	for i := 0; i+1 < len(xs); i++ {
		for j := 0; j+1 < len(ys); j++ {
			cell := RectF{X: xs[i], Y: ys[j], W: xs[i+1] - xs[i], H: ys[j+1] - ys[j]}
			cx, cy := cell.X+cell.W/2, cell.Y+cell.H/2
			if clip.Contains(cx, cy) && !coveredBy(covers, cx, cy) {
				out = append(out, cell)
			}
		}
	}
	return out
}

func coveredBy(covers []RectF, x, y float64) bool {
	for _, c := range covers {
		if c.Contains(x, y) {
			return true
		}
	}
	return false
}

func px(f float64) string { return ftoa(f) + "px" }

// cssRGBA is a colour as CSS reads it.
func cssRGBA(c platform.Color) string {
	return "rgba(" + itoa(int(c.R)) + "," + itoa(int(c.G)) + "," + itoa(int(c.B)) + "," + ftoa(float64(c.A)/255) + ")"
}

// listen reports the page's load to the handler, unless the view has closed. A
// load before the app asked for a page is the browser's own, of the empty frame
// (a browser may fire it after the frame is inserted), and is not reported.
func (v *webView) listen() {
	f := js.FuncOf(func(js.Value, []js.Value) any {
		v.s.w.b.sync(func() {
			if v.closed || !v.started {
				return
			}
			v.loading = false
			v.h.LoadFinished()
		})
		return nil
	})
	v.funcs = append(v.funcs, f)
	v.el.Call("addEventListener", "load", f)
}

// releaseFuncs removes the view's listeners and releases their functions after
// their last calls, which find the view closed, have run, as the surface does.
func (v *webView) releaseFuncs() {
	funcs := v.funcs
	v.funcs = nil
	for _, f := range funcs {
		v.el.Call("removeEventListener", "load", f)
	}
	var release js.Func
	release = js.FuncOf(func(js.Value, []js.Value) any {
		for _, f := range funcs {
			f.Release()
		}
		release.Release()
		return nil
	})
	js.Global().Call("setTimeout", release, 1000)
}

// WebViewHandle has no native object to return: a browser's iframe has none.
func (v *webView) WebViewHandle() uintptr { return 0 }

// LoadURL loads a URL in the view, as an iframe's source.
func (v *webView) LoadURL(url string) {
	if v.closed {
		return
	}
	v.el.Call("removeAttribute", "srcdoc")
	v.el.Set("src", url)
	v.url, v.srcdoc, v.loading, v.started = url, false, true, true
}

// LoadHTML loads HTML in the view. The browser gives an HTML document the URL of
// the page that holds it, so baseURL is only what URL reports.
func (v *webView) LoadHTML(html, baseURL string) {
	if v.closed {
		return
	}
	v.el.Call("removeAttribute", "src")
	v.el.Set("srcdoc", html)
	v.url, v.srcdoc, v.loading, v.started = baseURL, true, true, true
}

// LoadFile is not available in a browser: a page loads only from a URL or from
// HTML.
func (v *webView) LoadFile(path, readAccessDir string) {
	log.Printf("mygo: browsers load no file into a web view (%s)", path)
}

// Reload loads the page again: a URL page loads the URL LoadURL gave, not the
// address the frame has moved to. The cache is the browser's.
func (v *webView) Reload(ignoreCache bool) {
	if v.closed {
		return
	}
	v.loading = true
	if v.srcdoc {
		v.el.Set("srcdoc", v.el.Get("srcdoc"))
		return
	}
	v.el.Set("src", v.url)
}

// StopLoading does nothing: a browser does not stop a frame's load from outside.
func (v *webView) StopLoading() {}

// GoBack does nothing: the history of a frame of another origin is not the app's.
func (v *webView) GoBack() {}

// GoForward does nothing, as GoBack does.
func (v *webView) GoForward() {}

// CanGoBack reports false, as the history of a view is not reachable from the app.
func (v *webView) CanGoBack() bool { return false }

// CanGoForward reports false, as CanGoBack does.
func (v *webView) CanGoForward() bool { return false }

// URL returns the URL the view loads, or the base URL of the HTML it shows.
func (v *webView) URL() string { return v.url }

// IsLoading reports whether the page has not loaded since it was last loaded.
func (v *webView) IsLoading() bool { return v.loading }

// Eval runs code in the view's page when the page is the app's own, and only
// then: a browser runs no script of an untrusted page in the app's realm. The
// result is ignored, and an error is logged.
func (v *webView) Eval(code string) {
	if v.closed {
		return
	}
	if v.untrusted {
		log.Print("mygo: an untrusted web view runs no script from the app")
		return
	}
	win := v.el.Get("contentWindow")
	if win.IsNull() || win.IsUndefined() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mygo: web view script: %v", r)
		}
	}()
	win.Call("eval", code)
}

// SetUserScripts does nothing: the browser injects no scripts into a frame, so
// the scripts of WindowOptions.UserScripts run only in the app's own pages.
func (v *webView) SetUserScripts([]platform.UserScript) {}

// SetContentRules does nothing: the browser filters no requests of a frame.
func (v *webView) SetContentRules([]any) {}

// CallAsyncFunction reports that a browser runs no such function in a frame.
func (v *webView) CallAsyncFunction(body string, cb func(string, error)) {
	v.s.w.b.sync(func() { cb("", errUnsupported) })
}

// SetZoom records the zoom the app asked for; the browser's zoom is the user's.
func (v *webView) SetZoom(factor float64) { v.zoom = factor }

// Zoom returns the zoom the app asked for.
func (v *webView) Zoom() float64 { return v.zoom }

// SetUserAgent records the user agent the app asked for; a browser does not let
// a frame change its own.
func (v *webView) SetUserAgent(ua string) { v.userAgent = ua }

// UserAgent returns the user agent the app asked for.
func (v *webView) UserAgent() string { return v.userAgent }

// OpenDevTools does nothing: the browser's own developer tools serve the page.
func (v *webView) OpenDevTools() {}

// CloseDevTools does nothing, as OpenDevTools does.
func (v *webView) CloseDevTools() {}

// IsDevToolsOpened reports false: the app has no developer tools of its own in a
// browser.
func (v *webView) IsDevToolsOpened() bool { return false }

// CapturePage reports that a browser takes no snapshot of a frame.
func (v *webView) CapturePage(cb func([]byte, error)) {
	v.s.w.b.sync(func() { cb(nil, errUnsupported) })
}

// PrintToPDF reports that a browser prints no frame to a document.
func (v *webView) PrintToPDF(_ platform.PDFOptions, cb func([]byte, error)) {
	v.s.w.b.sync(func() { cb(nil, errUnsupported) })
}

// DroppedFiles reports no files: a drop on a frame is the frame's own.
func (v *webView) DroppedFiles() []string { return nil }

// Print does nothing: the browser prints the app's window, not a frame.
func (v *webView) Print() {}

// Focus gives the view the keyboard.
func (v *webView) Focus() {
	if !v.closed {
		v.el.Call("focus")
	}
}

// TabInto gives the view the keyboard too: the page's own elements take Tab
// from there.
func (v *webView) TabInto(bool) {
	if !v.closed {
		v.el.Call("focus")
	}
}

// SetBackgroundColor paints the view before its page does.
func (v *webView) SetBackgroundColor(c platform.Color) {
	if !v.closed {
		v.el.Get("style").Set("background", cssRGBA(c))
	}
}

// Close destroys the view; its handler hears no more of it, and a keyboard the
// view had goes back to the content.
func (v *webView) Close() {
	if v.closed {
		return
	}
	v.closed = true
	v.giveUpKeyboard()
	v.el.Call("remove")
	v.s.views.(*webViews).drop(v)
	v.releaseFuncs()
}
