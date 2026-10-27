//go:build windows

package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestTmpIframeRealm(t *testing.T) {
	port, packets := udpSink(t)
	v := untrustedScreenView(t)
	got, err := mygo.EvalAs[string](v.Page(), `(async () => {
		const out = [];
		const m = document.createElement("meta");
		m.httpEquiv = "Content-Security-Policy"; m.content = "webrtc 'block'";
		document.head.appendChild(m);
		const f = document.body.appendChild(document.createElement("iframe"));
		const C = f.contentWindow.RTCPeerConnection;
		out.push("iframe ctor=" + typeof C);
		try {
			const pc = new C({ iceServers: [{ urls: "stun:127.0.0.1:`+itoa(port)+`" }] });
			pc.createDataChannel("x");
			await pc.setLocalDescription(await pc.createOffer());
			out.push("gathering");
		} catch (e) { out.push("error:" + e.name + ":" + e.message) }
		await new Promise((r) => setTimeout(r, 1500));
		return out.join(" ");
	})()`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s packets=%d", got, packets.Load())
}

func itoa(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
