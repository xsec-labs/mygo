//go:build windows && (amd64 || arm64)

package windows

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestSchemeURLMapping(t *testing.T) {
	w := &window{opts: &platform.WindowOptions{Schemes: []string{"mygo", "xsec-plugin"}}}
	for app, web := range map[string]string{
		"mygo://localhost/index.html":                         "http://mygo.localhost/index.html",
		"xsec-plugin://localhost/a.js?x=1":                    "http://xsec-plugin.localhost/a.js?x=1",
		"xsec-plugin://com.xsec.workspace.approvals/index.js": "http://com.xsec.workspace.approvals.xsec-plugin.localhost/index.js",
		"xsec-plugin://a-b/x":                                 "http://a-b.xsec-plugin.localhost/x",
		"https://example.com/":                                "https://example.com/",
	} {
		if got := w.webURL(app); got != web {
			t.Errorf("webURL(%q) = %q, want %q", app, got, web)
		}
		if got := w.appURL(web); got != app {
			t.Errorf("appURL(%q) = %q, want %q", web, got, app)
		}
	}
	if got := w.appURL("http://evil.example.localhost/"); got != "http://evil.example.localhost/" {
		t.Errorf("an unrelated .localhost host must stay as it is, got %q", got)
	}
}
