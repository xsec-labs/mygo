//go:build !js

package e2e

import (
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// untrustedScreenView shows an untrusted page in a window of native UI.
func untrustedScreenView(t *testing.T) *mygo.WebView {
	t.Helper()
	var view atomic.Pointer[mygo.WebView]
	w := newWindow(t, mygo.WindowOptions{Title: "Untrusted screen capture", Width: 400, Height: 300, Content: ui.View(func(c *ui.Context) {
		if v := view.Load(); v != nil {
			ui.WebView(c, v).Grow(1)
		}
	})})
	v, err := w.NewWebView(mygo.WebViewOptions{Untrusted: &mygo.UntrustedOptions{OnMessage: func(*mygo.Page, string) {}}})
	if err != nil {
		t.Fatal(err)
	}
	w.Update(func() { view.Store(v) })
	// A page of a registered scheme: a web view only exposes mediaDevices to
	// a secure context. Windows serves the scheme as http://app.localhost/.
	if err := v.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, v.Page(), "(location.protocol === 'app:' || location.hostname === 'app.localhost') && document.readyState === 'complete'")
	return v
}

// TestUntrustedPageCannotCaptureScreen: an untrusted page asking for the
// screen, in a click, is refused outright. The guard that answers is the
// script's; the backends' own switches are checked where they exist (see the
// darwin test).
func TestUntrustedPageCannotCaptureScreen(t *testing.T) {
	v := untrustedScreenView(t)
	if has, _ := mygo.EvalAs[bool](v.Page(), "typeof MediaDevices !== 'undefined'"); !has {
		t.Skip("this web view has no MediaDevices: nothing to ask")
	}
	// Called the way the page would, from a click, on the object it has or
	// on a stand-in where the web view hides navigator.mediaDevices (a macOS
	// process without an Info.plist): either way the answer is the guard's.
	got, err := mygo.EvalAs[string](v.Page(), `new Promise((resolve) => {
		const md = navigator.mediaDevices || Object.create(MediaDevices.prototype);
		const b = document.body.appendChild(document.createElement("button"));
		b.onclick = async () => {
			try { await md.getDisplayMedia({ video: true }); resolve("GRANTED") } catch (e) { resolve(e.name) }
		};
		b.click();
	})`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "NotAllowedError" {
		t.Errorf("getDisplayMedia in an untrusted page: %q, want NotAllowedError", got)
	}
	// The page cannot put the original back.
	restored, err := mygo.EvalAs[bool](v.Page(), `(() => {
		const d = Object.getOwnPropertyDescriptor(MediaDevices.prototype, "getDisplayMedia");
		try { Object.defineProperty(MediaDevices.prototype, "getDisplayMedia", { value: () => 1 }) } catch (e) {}
		return d.configurable || MediaDevices.prototype.getDisplayMedia() === 1;
	})()`)
	if err != nil || restored {
		t.Errorf("the page replaced the getDisplayMedia guard (%v, %v)", restored, err)
	}
}

// TestUntrustedPageCannotLocate: an untrusted page asking for the position
// gets PERMISSION_DENIED from the script's guard, at once, whatever the
// app's own location authorization is (the request never reaches the system).
func TestUntrustedPageCannotLocate(t *testing.T) {
	v := untrustedScreenView(t)
	if has, _ := mygo.EvalAs[bool](v.Page(), "typeof navigator.geolocation !== 'undefined'"); !has {
		t.Skip("this web view has no geolocation: nothing to ask")
	}
	got, err := mygo.EvalAs[string](v.Page(), `new Promise((resolve) => {
		const t0 = performance.now();
		const done = (r) => resolve(r + "@" + (performance.now() - t0 < 2000 ? "fast" : "slow"));
		navigator.geolocation.getCurrentPosition(() => done("GRANTED"), (e) => done("code" + e.code), { timeout: 5000 });
	})`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "code1@fast" {
		t.Errorf("getCurrentPosition in an untrusted page: %q, want code1@fast", got)
	}
	watched, err := mygo.EvalAs[string](v.Page(), `new Promise((resolve) => {
		navigator.geolocation.watchPosition(() => resolve("GRANTED"), (e) => resolve("code" + e.code));
	})`)
	if err != nil || watched != "code1" {
		t.Errorf("watchPosition in an untrusted page: %q (%v), want code1", watched, err)
	}
	restored, err := mygo.EvalAs[bool](v.Page(), `(() => {
		const d = Object.getOwnPropertyDescriptor(Geolocation.prototype, "getCurrentPosition");
		try { Object.defineProperty(Geolocation.prototype, "getCurrentPosition", { value: () => 1 }) } catch (e) {}
		return d.configurable || Geolocation.prototype.getCurrentPosition() === 1;
	})()`)
	if err != nil || restored {
		t.Errorf("the page replaced the geolocation guard (%v, %v)", restored, err)
	}
}
