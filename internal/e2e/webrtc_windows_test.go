//go:build windows

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestWebViewsSendNoDirectUDP: the WebView2 environment is the app's, shared
// by every web view, and it forbids WebRTC UDP that does not go through a
// proxy. A page that is not untrusted, with its constructors in place, still
// sends nothing to a STUN server: the switch holds without the script guard.
func TestWebViewsSendNoDirectUDP(t *testing.T) {
	port, packets := udpSink(t)
	w := newWindow(t, mygo.WindowOptions{Title: "WebRTC", Width: 400, Height: 300})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, w.Page(), "document.readyState === 'complete'")
	if has, _ := mygo.EvalAs[bool](w.Page(), "typeof RTCPeerConnection === 'function'"); !has {
		t.Fatal("the web view has no RTCPeerConnection: the test would prove nothing")
	}
	got, err := mygo.EvalAs[string](w.Page(), gatherJS(port))
	if err != nil {
		t.Fatal(err)
	}
	if got != "gathering,gathering" {
		t.Logf("gathering in a trusted page: %q", got)
	}
	if n := packets.Load(); n != 0 {
		t.Errorf("a web view sent %d UDP packets to a STUN server", n)
	}
}
