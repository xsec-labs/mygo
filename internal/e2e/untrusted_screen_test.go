//go:build !js

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

const screenCapturePage = `<!doctype html><html><body></body></html>`

// untrustedScreenView shows an untrusted page in a window of its own.
func untrustedScreenView(t *testing.T) *mygo.WebView {
	t.Helper()
	w := newWindow(t, mygo.WindowOptions{Title: "Untrusted screen capture", Width: 400, Height: 300})
	v, err := w.NewWebView(mygo.WebViewOptions{Untrusted: &mygo.UntrustedOptions{OnMessage: func(*mygo.Page, string) {}}})
	if err != nil {
		t.Fatal(err)
	}
	v.Page().LoadHTML(screenCapturePage, "")
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
		t.Skip("this web view exposes no navigator.mediaDevices (a macOS process without an Info.plist): nothing to ask")
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
