//go:build windows && (amd64 || arm64)

package windows

import (
	"strings"
	"testing"
	"unsafe"
)

// TestChromiumLanguage checks the tags WebView2's browser is given for
// Chinese, which Chromium names by region.
func TestChromiumLanguage(t *testing.T) {
	for tag, want := range map[string]string{
		"zh-Hans":    "zh-CN",
		"zh-Hant":    "zh-TW",
		"zh-Hant-HK": "zh-HK",
		"zh-TW":      "zh-TW",
		"zh-Hans-SG": "zh-CN",
		"zh":         "zh-CN",
		"en-GB":      "en-GB",
		"sr-Latn":    "sr-Latn",
	} {
		if got := chromiumLanguage(tag); got != want {
			t.Errorf("chromiumLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
}

// TestEnvOptionsBrowserArguments checks that the environment is created with
// the switch that keeps WebRTC from sending UDP to arbitrary hosts, with and
// without a language, since an untrusted page's peer connection is a way out
// that its CSP does not cover.
func TestEnvOptionsBrowserArguments(t *testing.T) {
	for _, language := range []string{"", "en-US"} {
		o := newEnvOptions(language)
		var p uintptr
		// get_AdditionalBrowserArguments is the first method after IUnknown.
		if hr := comCall(o, 3, uintptr(unsafe.Pointer(&p))); failed(hr) {
			t.Fatalf("language %q: get_AdditionalBrowserArguments failed: %#x", language, hr)
		}
		got := takeWstr(p)
		release(o)
		if !strings.Contains(got, "--force-webrtc-ip-handling-policy=disable_non_proxied_udp") {
			t.Errorf("language %q: browser arguments %q do not disable non-proxied WebRTC UDP", language, got)
		}
	}
}
