//go:build windows

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestTmpIframeRealm(t *testing.T) {
	v := untrustedScreenView(t)
	got, err := mygo.EvalAs[string](v.Page(), `(async () => {
		const out = [];
		const mk = (name, fn) => { const f = document.createElement("iframe"); fn(f); document.body.appendChild(f); return [name, f] };
		const frames = [mk("blank", () => {}), mk("srcblank", (f) => { f.src = "about:blank" }), mk("srcdoc", (f) => { f.srcdoc = "<p>x</p>" })];
		const rd = (f) => { try { return typeof f.contentWindow.RTCPeerConnection } catch (e) { return "err:" + e.name } };
		for (const [n, f] of frames) out.push(n + "@0=" + rd(f));
		await new Promise((r) => setTimeout(r, 500));
		for (const [n, f] of frames) out.push(n + "@500=" + rd(f));
		out.push("main=" + typeof RTCPeerConnection);
		return out.join(" ");
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(got)
}
