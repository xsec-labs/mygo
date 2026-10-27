//go:build !js

package e2e

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
)

// udpSink listens on a loopback UDP port and counts the datagrams it gets.
func udpSink(t *testing.T) (port int, packets *atomic.Int64) {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	packets = new(atomic.Int64)
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := c.ReadFrom(buf); err != nil {
				return
			}
			packets.Add(1)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr).Port, packets
}

// gatherJS starts ICE gathering against a STUN server, as a page would to
// send data to a host of its choice, and reports what it could do.
func gatherJS(port int) string {
	return fmt.Sprintf(`(async () => {
		const out = [];
		const realms = [window];
		const f = document.body.appendChild(document.createElement("iframe"));
		realms.push(f.contentWindow);
		for (const w of realms) {
			const C = w.RTCPeerConnection || w.webkitRTCPeerConnection;
			if (typeof C !== "function") { out.push("no-constructor"); continue }
			try {
				const pc = new C({ iceServers: [{ urls: "stun:127.0.0.1:%d" }] });
				pc.createDataChannel("x");
				await pc.setLocalDescription(await pc.createOffer());
				out.push("gathering");
			} catch (e) { out.push("error:" + e.name) }
		}
		await new Promise((r) => setTimeout(r, 1500));
		return out.join(",");
	})()`, port)
}

// TestUntrustedPageCannotPeerConnect: a peer connection with a STUN or TURN
// server is UDP to any host and a name lookup, which the page's CSP and the
// navigation filters do not see. An untrusted page, in its own realm and in a
// frame it makes, gets no constructor and sends no packet, and cannot put one
// back.
func TestUntrustedPageCannotPeerConnect(t *testing.T) {
	port, packets := udpSink(t)
	v := untrustedScreenView(t)
	got, err := mygo.EvalAs[string](v.Page(), gatherJS(port))
	if err != nil {
		t.Fatal(err)
	}
	if got != "no-constructor,no-constructor" {
		t.Errorf("peer connections in an untrusted page: %q, want no-constructor in both realms", got)
	}
	if n := packets.Load(); n != 0 {
		t.Errorf("the untrusted page's peer connections sent %d UDP packets", n)
	}
	restored, err := mygo.EvalAs[bool](v.Page(), `(() => {
		try { Object.defineProperty(window, "RTCPeerConnection", { value: function () {} }) } catch (e) {}
		return typeof RTCPeerConnection === "function";
	})()`)
	if err != nil || restored {
		t.Errorf("the page put a peer connection constructor back (%v, %v)", restored, err)
	}
}
