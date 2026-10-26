//go:build !js

package mygo

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/wait"
	"github.com/egoist/mygo/ui"
)

type untrustedProbe struct{ calls atomic.Int64 }

func (p *untrustedProbe) Touch(ctx context.Context) string {
	p.calls.Add(1)
	return "reached"
}

var (
	probeOnce  sync.Once
	probeShare *untrustedProbe
)

// untrustedProbeOnce binds the probe once: a service name binds once per
// process, and tests may run with -count > 1.
func untrustedProbeOnce() *untrustedProbe {
	probeOnce.Do(func() {
		probeShare = &untrustedProbe{}
		BindAs("UntrustedProbe", probeShare)
	})
	return probeShare
}

var canaryEvent = NewEvent[string]("untrusted-test-canary")

// untrustedWebView makes an untrusted web view in a window of native UI, its
// page loaded, and returns it with its fake page.
func untrustedWebView(t *testing.T, o UntrustedOptions) (*Window, *WebView, *fake.WebView) {
	t.Helper()
	var v *WebView
	w, _, s := contentWindow(t, func(c *ui.Context) {
		ui.Column(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if v != nil {
				ui.WebView(c, v).Grow(1)
			}
		})
	})
	v, err := w.NewWebView(WebViewOptions{Untrusted: &o})
	if err != nil {
		t.Fatal(err)
	}
	views := s.WebViews()
	if len(views) != 1 {
		t.Fatalf("%d web views", len(views))
	}
	fv := views[0]
	onMain(func() { fv.H.NavigationCommitted("xsec-plugin://example/index.html") })
	return w, v, fv
}

func untrustedSecret(fv *fake.WebView) string {
	scripts := fv.Window.Opts.UserScripts
	if len(scripts) != 1 {
		panic("an untrusted page gets exactly one script")
	}
	m := regexp.MustCompile(`const S="([^"]+)"`).FindStringSubmatch(scripts[0].Source)
	if m == nil {
		panic("no secret in the untrusted script")
	}
	return m[1]
}

func untrustedPost(fv *fake.WebView, msg string) {
	s := untrustedSecret(fv)
	onMain(func() { fv.Window.H.Message(s + msg) })
}

// The page must not be able to swap the native postMessage to be handed the
// secret: the script binds it at document start.
func TestUntrustedScriptBindsPostMessageAtStart(t *testing.T) {
	_, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	src := fv.Window.Opts.UserScripts[0].Source
	if !strings.Contains(src, "h.postMessage.bind(h)") || strings.Contains(src, "h.postMessage(m)") {
		t.Errorf("the script looks postMessage up on every call: %s", src)
	}
}

func TestUntrustedPageGetsNoBridge(t *testing.T) {
	_, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	src := fv.Window.Opts.UserScripts[0].Source
	for _, bad := range []string{"__MYGO_CONFIG__", "window.mygo", `"mygo"`, "mygo:window"} {
		if bad == `"mygo"` {
			continue // the message handler's name, which the page needs to post
		}
		if strings.Contains(src, bad) {
			t.Errorf("the untrusted script mentions %q", bad)
		}
	}
	if !strings.Contains(src, "xsecHost") {
		t.Error("the untrusted script defines no xsecHost")
	}
}

func TestUntrustedPageCannotCallBoundMethodsOrWindowControls(t *testing.T) {
	probe := untrustedProbeOnce()
	w, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	w.SetTitle("before")
	untrustedPost(fv, `{"t":"call","id":1,"k":"x","m":"UntrustedProbe.Touch","a":[]}`)
	untrustedPost(fv, `{"t":"call","id":2,"k":"x","m":"mygo:window.SetTitle","a":["pwned"]}`)
	untrustedPost(fv, `{"t":"call","id":3,"k":"x","m":"mygo:window.Close","a":[]}`)
	untrustedPost(fv, `{"t":"drag"}`)
	onMain(func() {})
	time.Sleep(100 * time.Millisecond)
	if n := probe.calls.Load(); n != 0 {
		t.Errorf("a bound method was reached %d times", n)
	}
	if got := w.Title(); got != "before" {
		t.Errorf("title = %q", got)
	}
	if w.IsDestroyed() {
		t.Error("the page closed the window")
	}
}

func TestUntrustedMessagesNeedTheSecret(t *testing.T) {
	got := make(chan string, 4)
	_, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(_ *Page, m string) { got <- m }})
	onMain(func() { fv.Window.H.Message("guess" + "Uhello") })
	untrustedPost(fv, "Uhello")
	select {
	case m := <-got:
		if m != "hello" {
			t.Errorf("message = %q", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
	select {
	case m := <-got:
		t.Errorf("a message without the secret got through: %q", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUntrustedMessageSizeLimit(t *testing.T) {
	got := make(chan string, 4)
	var page *Page
	_, v, fv := untrustedWebView(t, UntrustedOptions{MaxMessageBytes: 16, OnMessage: func(p *Page, m string) { page = p; got <- m }})
	untrustedPost(fv, "U"+strings.Repeat("x", 16))
	untrustedPost(fv, "U"+strings.Repeat("x", 17))
	untrustedPost(fv, "U"+strings.Repeat("x", 10<<20))
	if m := <-got; len(m) != 16 {
		t.Errorf("delivered %d bytes", len(m))
	}
	time.Sleep(100 * time.Millisecond)
	st, err := v.Page().UntrustedStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Delivered != 1 || st.TooLarge != 2 || len(got) != 0 {
		t.Errorf("stats = %+v", st)
	}
	if page != v.Page() {
		t.Error("OnMessage did not get the page of the web view")
	}
}

func TestUntrustedQueueIsBounded(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	started := make(chan struct{})
	_, v, fv := untrustedWebView(t, UntrustedOptions{MaxQueued: 2, OnMessage: func(*Page, string) {
		once.Do(func() { close(started) })
		<-release
	}})
	untrustedPost(fv, "Ufirst")
	<-started // the worker holds the first; the queue (2) takes the next two
	for i := 0; i < 50; i++ {
		untrustedPost(fv, "Uflood")
	}
	time.Sleep(100 * time.Millisecond)
	st, _ := v.Page().UntrustedStats()
	if st.Dropped != 48 {
		t.Errorf("dropped = %d, want 48 (stats %+v)", st.Dropped, st)
	}
	close(release)
}

func TestUntrustedPageHearsNoEvents(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	untrustedPost(fv, `{"t":"dom-ready"}`)
	onMain(func() {})
	fv.Window.ClearScripts()
	if err := canaryEvent.Broadcast("secret-payload"); err != nil {
		t.Fatal(err)
	}
	if err := v.Page().PostUntrusted("hello page"); err != nil {
		t.Fatal(err)
	}
	onMain(func() {})
	time.Sleep(100 * time.Millisecond)
	var all string
	for _, s := range fv.Window.Scripts() {
		all += s
	}
	if strings.Contains(all, "secret-payload") || strings.Contains(all, `"t":"event"`) {
		t.Errorf("an event reached the untrusted page: %s", all)
	}
	if !strings.Contains(all, `{"t":"u","p":"hello page"}`) {
		t.Errorf("PostUntrusted did not reach the page: %q", all)
	}
}

func TestPostUntrustedNeedsAnUntrustedPage(t *testing.T) {
	var show bool
	_, v, _, _ := webViewWindow(t, &show)
	if err := v.Page().PostUntrusted("x"); err == nil {
		t.Error("PostUntrusted worked on a trusted page")
	}
	if _, err := v.Page().UntrustedStats(); err == nil {
		t.Error("UntrustedStats worked on a trusted page")
	}
}

func TestUntrustedNeedsOnMessage(t *testing.T) {
	w, _, _ := contentWindow(t, func(c *ui.Context) { ui.Box(c) })
	if _, err := w.NewWebView(WebViewOptions{Untrusted: &UntrustedOptions{}}); err == nil {
		t.Error("an untrusted web view without OnMessage was made")
	}
}

// The untrusted policy: what a page may ask of the app besides messages is
// refused by default, whatever its origin. Each test below fails when its
// hook is removed (see ../mutation-check.sh).

func TestUntrustedPermissionsDeniedByDefault(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	for _, origin := range []string{"xsec-plugin://example", "https://example.com", "about:blank"} {
		if onMainValue(func() bool {
			return fv.Window.H.PermissionRequested([]string{"camera", "microphone", "geolocation"}, origin)
		}) {
			t.Errorf("permissions granted to %s", origin)
		}
	}
	// A handler the app set is still the app's call.
	v.Page().SetPermissionHandler(func(PermissionRequest) bool { return true })
	if !onMainValue(func() bool { return fv.Window.H.PermissionRequested([]string{"camera"}, "xsec-plugin://example") }) {
		t.Error("the app's permission handler was ignored")
	}
	st, _ := v.Page().UntrustedStats()
	if st.Blocked != 3 {
		t.Errorf("blocked = %d", st.Blocked)
	}
}

// Screen capture is the user's pick in the system's picker: a backend that
// reports it for an untrusted page gets a refusal even from an app handler
// that grants everything, and the refusal is counted.
func TestUntrustedScreenCaptureNeverGranted(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	ask := func() bool {
		return onMainValue(func() bool { return fv.Window.H.PermissionRequested([]string{"screen"}, "xsec-plugin://example") })
	}
	if ask() {
		t.Error("screen capture granted by default")
	}
	v.Page().SetPermissionHandler(func(PermissionRequest) bool { return true })
	if ask() {
		t.Error("the app's permission handler granted screen capture to an untrusted page")
	}
	st, _ := v.Page().UntrustedStats()
	if st.Blocked != 2 {
		t.Errorf("blocked = %d", st.Blocked)
	}
}

// The script that runs in an untrusted page, where the backend has no switch
// for screen capture, makes getDisplayMedia reject before the page can use it.
func TestUntrustedScriptRejectsGetDisplayMedia(t *testing.T) {
	src := untrustedScript("secret")
	for _, want := range []string{"getDisplayMedia", "NotAllowedError", "configurable:false", "MediaDevices"} {
		if !strings.Contains(src, want) {
			t.Errorf("the untrusted script lacks %q", want)
		}
	}
}

// The script that runs in an untrusted page denies position requests itself,
// so that a location authorization of the app is never reached.
func TestUntrustedScriptDeniesGeolocation(t *testing.T) {
	src := untrustedScript("secret")
	for _, want := range []string{"getCurrentPosition", "watchPosition", "PERMISSION_DENIED", "configurable:false", "Geolocation"} {
		if !strings.Contains(src, want) {
			t.Errorf("the untrusted script lacks %q", want)
		}
	}
}

func TestUntrustedNewWindowOpensNothing(t *testing.T) {
	var asked []string
	_, v, fv := untrustedWebView(t, UntrustedOptions{
		OnMessage:     func(*Page, string) {},
		OnOpenRequest: func(_ *Page, u string) { asked = append(asked, u) },
	})
	// The window open handler of a trusted page would open windows; for
	// this page it is not consulted.
	v.Page().SetWindowOpenHandler(func(WindowOpenRequest) *WindowOptions { return &WindowOptions{Width: 100, Height: 100} })
	before := len(fb.Windows())
	for i := 0; i < 20; i++ {
		if w := onMainValue(func() platform.Window {
			return fv.Window.H.NewWindow(platform.NewWindowRequest{URL: "https://evil.example/"})
		}); w != nil {
			t.Fatal("a window opened")
		}
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(fake.OpenedURLs()); n != 0 {
		t.Errorf("OpenExternal called %d times", n)
	}
	if len(fb.Windows()) != before {
		t.Error("a window was created")
	}
	if len(asked) != 20 || asked[0] != "https://evil.example/" {
		t.Errorf("OnOpenRequest got %d requests", len(asked))
	}
	st, _ := v.Page().UntrustedStats()
	if st.Blocked != 20 {
		t.Errorf("blocked = %d", st.Blocked)
	}
}

func TestUntrustedDownloadsCancelledByDefault(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	if p := onMainValue(func() string { return fv.Window.H.DownloadStarted("xsec-plugin://example/a.bin", "a.bin") }); p != "" {
		t.Errorf("download path = %q", p)
	}
	// A listener may opt in by naming a path.
	dst := filepath.Join(t.TempDir(), "ok.bin")
	off := v.Page().OnWillDownload(func(e *DownloadEvent) { e.Path = dst })
	defer off()
	if p := onMainValue(func() string { return fv.Window.H.DownloadStarted("xsec-plugin://example/a.bin", "a.bin") }); p != dst {
		t.Errorf("download path = %q, want the listener's", p)
	}
}

func TestUntrustedSchemeDownloadDoesNothingByDefault(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	var served atomic.Int64
	schemeOnce.Do(func() {
		Protocol.Handle("untrusted-dl", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { schemeServed.Add(1) }))
	})
	served.Store(schemeServed.Load())
	onMain(func() { fv.Window.H.SchemeDownload("untrusted-dl://x/file") })
	time.Sleep(100 * time.Millisecond)
	if schemeServed.Load() != served.Load() {
		t.Error("the scheme handler served an untrusted page's download")
	}
	if st, _ := v.Page().UntrustedStats(); st.Blocked != 1 {
		t.Errorf("blocked = %d", st.Blocked)
	}
}

func TestUntrustedNavigationStaysInItsOrigin(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	nav := func(u string, user bool) bool {
		return onMainValue(func() bool {
			return fv.Window.H.WillNavigate(platform.Navigation{URL: u, IsMainFrame: true, UserInitiated: user})
		})
	}
	if !nav("xsec-plugin://example/other.html", false) {
		t.Error("navigation inside the origin was blocked")
	}
	for _, u := range []string{"https://evil.example/", "xsec-plugin://other/index.html", "file:///etc/passwd", "about:blank", "javascript:alert(1)"} {
		if nav(u, true) || nav(u, false) {
			t.Errorf("navigation to %s was allowed", u)
		}
	}
	// A subframe is not the main frame's concern.
	if !onMainValue(func() bool {
		return fv.Window.H.WillNavigate(platform.Navigation{URL: "https://frame.example/", IsMainFrame: false})
	}) {
		t.Error("a subframe navigation was refused")
	}
	// A page that still got to another origin (a redirect) is cut off.
	got := make(chan string, 4)
	_ = got
	onMain(func() { fv.Window.H.NavigationCommitted("https://evil.example/") })
	untrustedPost(fv, "Ugone")
	time.Sleep(100 * time.Millisecond)
	st, _ := v.Page().UntrustedStats()
	if st.Delivered != 0 {
		t.Errorf("a message from another origin was delivered: %+v", st)
	}
}

func TestUntrustedPageIsNeverTrusted(t *testing.T) {
	for _, u := range []string{"xsec-plugin://example/", "about:blank", "file:///x"} {
		w, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
		_ = w
		onMain(func() { fv.Window.H.NavigationCommitted(u) })
		if onMainValue(func() bool { return pageWindowOf(fv).trusted }) {
			t.Errorf("a page at %s is trusted", u)
		}
	}
}

func TestUntrustedFileDropsAndTitleStayOut(t *testing.T) {
	w, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	w.SetTitle("host title")
	var dropped atomic.Int64
	w.OnFileDrop(func(*FileDropEvent) { dropped.Add(1) })
	untrustedPost(fv, `{"t":"dom-ready"}`)
	onMain(func() {})
	fv.Window.ClearScripts()

	// The drop reaches the app's own listener, not the page.
	pw := pageWindowOf(fv)
	fv.Window.Dropped = []string{"/home/user/secret.txt"}
	untrustedPost(fv, `{"t":"drop","x":1,"y":1}`)
	onMain(func() { pw.filesDropped([]string{"/home/user/secret.txt"}, 1, 1) })
	time.Sleep(100 * time.Millisecond)
	for _, s := range fv.Window.Scripts() {
		if strings.Contains(s, "secret.txt") {
			t.Errorf("a dropped file's path reached the page: %s", s)
		}
	}

	// document.title of the page is not the window's title, and its
	// listeners are still told.
	var told atomic.Int64
	v.Page().OnPageTitleUpdated(func(*TitleEvent) { told.Add(1) })
	onMain(func() { fv.Window.H.TitleChanged("pwned by plugin") })
	onMain(func() {})
	if got := w.Title(); got != "host title" {
		t.Errorf("title = %q", got)
	}
	// (SetTitle of a web view's native window is a no-op in this mygo, so
	// the host's title cannot change either way; the page's own title
	// field must stay unset, so that a later change cannot leak it.)
	if got := onMainValue(func() string { return pageWindowOf(fv).title }); got != "" {
		t.Errorf("the page's title = %q", got)
	}
	if told.Load() != 1 {
		t.Errorf("listeners told %d times", told.Load())
	}
}

var (
	schemeOnce   sync.Once
	schemeServed atomic.Int64
)

func TestUntrustedDOMReadyOnlyOncePerDocument(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	untrustedPost(fv, `{"t":"dom-ready"}`)
	untrustedPost(fv, `{"t":"dom-ready"}`) // the same document: not again
	onMain(func() {})
	if st, _ := v.Page().UntrustedStats(); st.Malformed != 1 {
		t.Errorf("malformed = %d", st.Malformed)
	}
}

// After a reload or a navigation inside the origin, the new document says
// dom-ready again and must hear the app again.
func TestUntrustedPageHearsTheAppAfterReloadAndSameOriginNavigation(t *testing.T) {
	_, v, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	untrustedPost(fv, `{"t":"dom-ready"}`)
	onMain(func() {})
	for i, url := range []string{
		"xsec-plugin://example/index.html",       // reload
		"xsec-plugin://example/other.html?x=1",   // same-origin navigation
		"xsec-plugin://example/other.html?x=1#h", // and another
	} {
		fv.Window.ClearScripts()
		onMain(func() { fv.Window.H.NavigationCommitted(url) })
		// Sent while the new document is not ready: held, then delivered.
		msg := "after-" + string(rune('a'+i))
		if err := v.Page().PostUntrusted(msg); err != nil {
			t.Fatal(err)
		}
		onMain(func() {})
		for _, s := range fv.Window.Scripts() {
			if strings.Contains(s, msg) {
				t.Fatalf("%s: delivered before the new document was ready", url)
			}
		}
		untrustedPost(fv, `{"t":"dom-ready"}`)
		onMain(func() {})
		var all string
		for _, s := range fv.Window.Scripts() {
			all += s
		}
		if !strings.Contains(all, msg) {
			t.Errorf("%s: the new document never heard the app (scripts %q)", url, all)
		}
	}
	if st, _ := v.Page().UntrustedStats(); st.Malformed != 0 {
		t.Errorf("a new document's dom-ready counted as malformed: %+v", st)
	}
}

func TestUntrustedWebViewAsksForNoClipboardOrFullScreen(t *testing.T) {
	_, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})
	if !fv.Window.Opts.Untrusted {
		t.Error("the platform web view was not told the page is untrusted (clipboard, full screen)")
	}
	var show bool
	_, _, _, tfv := webViewWindow(t, &show)
	if tfv.Window.Opts.Untrusted {
		t.Error("a trusted web view was marked untrusted")
	}
}

func TestUntrustedDestroyReleasesTheWorker(t *testing.T) {
	var w *Window
	var views []*WebView
	w, _, s := contentWindow(t, func(c *ui.Context) { ui.Box(c) })
	_ = s
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	base := runtime.NumGoroutine()
	for i := 0; i < 1000; i++ {
		v, err := w.NewWebView(WebViewOptions{Untrusted: &UntrustedOptions{OnMessage: func(*Page, string) {}}})
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, v)
		fvs := s.WebViews()
		fv := fvs[len(fvs)-1]
		untrustedPost(fv, "Uhello") // starts the worker
		// Wait for it to run before destroying.
		for j := 0; j < 200; j++ {
			if st, _ := v.Page().UntrustedStats(); st.Delivered == 1 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		v.Destroy()
		// A message after Destroy goes nowhere, and does not panic.
		onMain(func() { fv.Window.H.Message(untrustedSecret(fv) + "Ulate") })
	}
	var n int
	for i := 0; i < 100; i++ {
		runtime.GC()
		n = runtime.NumGoroutine()
		if n <= base+5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n > base+5 {
		t.Errorf("goroutines = %d after 1000 destroyed web views (baseline %d)", n, base)
	}
}

// pageWindowOf returns the page's Window of a fake web view.
func pageWindowOf(fv *fake.WebView) *Window { return fv.Window.H.(*windowHandler).w }

// The origin of an untrusted page is a custom scheme like the app's own, but
// it is a third party's: it gets no CORS headers, so it cannot read the app's
// frontend or the files of another plugin.
func TestUntrustedPageGetsNoCORSHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("data")) })
	if err := Protocol.Handle("assets", mux); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("assets")
	if err := Protocol.Handle("xsec-plugin", mux); err != nil {
		t.Fatal(err)
	}
	defer Protocol.Unhandle("xsec-plugin")
	defer func(v bool) { schemeHosts = v }(schemeHosts)
	schemeHosts = true // as on Windows
	_, _, fv := untrustedWebView(t, UntrustedOptions{OnMessage: func(*Page, string) {}})

	for _, origin := range []string{
		"xsec-plugin://example",                // the page itself
		"xsec-plugin://other",                  // another plugin
		"mygo://localhost",                     // the frontend
		"http://mygo.localhost",                // the frontend, as WebView2 serves it
		"http://example.xsec-plugin.localhost", // the page, as WebView2 serves it
	} {
		rec := &recorder{finished: make(chan struct{})}
		h := http.Header{}
		h.Set("Origin", origin)
		req := &platform.SchemeRequest{Context: context.Background(), Method: "GET", URL: "assets://localhost/data", Header: h, Responder: rec}
		onMain(func() { fv.Window.H.SchemeRequest(req) })
		select {
		case <-rec.finished:
		case <-time.After(wait.Guard):
			t.Fatal("request did not finish")
		}
		if rec.status != 200 {
			t.Errorf("%s: status %d", origin, rec.status)
		}
		if got := rec.header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want none", origin, got)
		}
	}
}
