//go:build darwin

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/darwin"
)

// TestUntrustedWebViewHasNoScreenCapture: WebKit raises the system's screen
// sharing picker for getDisplayMedia without asking the app, so an untrusted
// web view must have screen capture off in its preferences, not only a
// script that rejects the call.
func TestUntrustedWebViewHasNoScreenCapture(t *testing.T) {
	untrustedScreenView(t)
	var untrusted []bool
	mygo.RunOnMain(func() { untrusted, _ = darwin.TestScreenCaptureOfWebViews() })
	if len(untrusted) == 0 {
		t.Fatal("no untrusted web view found")
	}
	for i, on := range untrusted {
		if on {
			t.Errorf("untrusted web view %d has screen capture enabled", i)
		}
	}
}

// TestUntrustedWebViewHasNoPeerConnection: WebKit's own switch for WebRTC is
// off in the preferences of an untrusted web view, not only the script's
// guard.
func TestUntrustedWebViewHasNoPeerConnection(t *testing.T) {
	untrustedScreenView(t)
	var untrusted []bool
	mygo.RunOnMain(func() { untrusted, _ = darwin.TestPeerConnectionOfWebViews() })
	if len(untrusted) == 0 {
		t.Skip("WKPreferences has no peer connection switch here: the script's guard is the only one")
	}
	for i, on := range untrusted {
		if on {
			t.Errorf("untrusted web view %d has WebRTC enabled", i)
		}
	}
}
