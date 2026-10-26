//go:build !js

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// TestUntrustedPageNavigationSendsNoRequest: an untrusted page that navigates
// away is refused, and the refused navigation does not reach the server
// either: a URL can carry data out, so a request the page's script started
// must not leave the app because the response is thrown away. The page has no
// content security policy, so only the backend is in the way.
func TestUntrustedPageNavigationSendsNoRequest(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.RequestURI())
		mu.Unlock()
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	got := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hits...)
	}

	v := untrustedScreenView(t)
	before, _ := mygo.EvalAs[string](v.Page(), "location.href")
	base := fmt.Sprintf("http://127.0.0.1:%d/hit", port)
	for _, c := range []struct{ name, js string }{
		{"location.href", `location.href = "` + base + `?href"`},
		{"location.replace", `location.replace("` + base + `?replace")`},
		{"anchor", `const a = document.createElement("a"); a.href = "` + base + `?anchor"; document.body.appendChild(a); a.click()`},
		{"meta refresh", `const m = document.createElement("meta"); m.httpEquiv = "refresh"; m.content = "0;url=` + base + `?meta"; document.head.appendChild(m)`},
		{"form", `const f = document.createElement("form"); f.action = "` + base + `?form"; document.body.appendChild(f); f.submit()`},
		{"window.open", `window.open("` + base + `?open")`},
	} {
		if _, err := mygo.EvalAs[any](v.Page(), c.js); err != nil {
			t.Logf("%s: %v", c.name, err)
		}
		time.Sleep(700 * time.Millisecond)
		if h := got(); len(h) > 0 {
			t.Fatalf("after %s the server got requests %v", c.name, h)
		}
	}
	if after, _ := mygo.EvalAs[string](v.Page(), "location.href"); after != before {
		t.Errorf("the page moved: %q -> %q", before, after)
	}
}
