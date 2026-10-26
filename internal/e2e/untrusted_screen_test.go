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
	// a secure context.
	if err := v.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, v.Page(), "document.readyState === 'complete'")
	return v
}

// TestUntrustedPageCannotCaptureScreen: an untrusted page asking for the
// screen, in a click, is refused outright. The guard that answers is the
// script's; the backends' own switches are checked where they exist (see the
// darwin test).
func TestUntrustedPageCannotCaptureScreen(t *testing.T) {
	v := untrustedScreenView(t)
	if has, _ := mygo.EvalAs[bool](v.Page(), "!!(navigator.mediaDevices)"); !has {
		state, _ := mygo.EvalAs[string](v.Page(), "location.href + ' secure=' + isSecureContext + ' MediaDevices=' + typeof MediaDevices + ' RTC=' + typeof RTCPeerConnection + ' guard=' + (typeof MediaDevices !== 'undefined' && !!Object.getOwnPropertyDescriptor(MediaDevices.prototype, 'getDisplayMedia'))")
		t.Skipf("this web view exposes no navigator.mediaDevices (%s; a macOS process without an Info.plist has none): nothing to ask", state)
	}
	got, err := mygo.EvalAs[string](v.Page(), `new Promise((resolve) => {
		const b = document.body.appendChild(document.createElement("button"));
		b.onclick = async () => {
			try { await navigator.mediaDevices.getDisplayMedia({ video: true }); resolve("GRANTED") } catch (e) { resolve(e.name) }
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
		return d.configurable || navigator.mediaDevices.getDisplayMedia() === 1;
	})()`)
	if err != nil || restored {
		t.Errorf("the page replaced the getDisplayMedia guard (%v, %v)", restored, err)
	}
}
