package mygo

import (
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// UntrustedOptions configure a web view whose page the app does not trust
// (WebViewOptions.Untrusted): it runs third party code, such as a plugin's.
//
// The page gets one object, window.xsecHost, and nothing of the bridge:
//
//	xsecHost.postMessage(text)    // page -> app, text only
//	xsecHost.addListener(fn)      // app -> page; returns a function removing fn
//
// The page cannot call bound methods or mygo:window.* (the window controls),
// does not hear events (Event.Emit, Broadcast), and sees neither the
// preferences nor the locale. A message is a string, so that nothing but
// the app's own code decodes it.
//
// The messages of the script carry a secret, which only tells them from other
// noise posted to the same native handler: it is not a boundary, and a page
// that gets hold of it can only send what xsecHost sends.
//
// The app tells which page sent a message by the page: OnMessage gets the
// Page of the web view, which only the app created.
//
// Everything else a page can ask of the app is refused by default, whatever
// the origin of the page, so that an untrusted page needs no extra care from
// the app: permission requests (camera, microphone, location, ...) are
// denied unless the app set Page.SetPermissionHandler; downloads are
// cancelled unless an OnWillDownload listener sets a Path; window.open and
// target=_blank open nothing (Page.SetWindowOpenHandler is ignored; see
// OnOpenRequest); the main frame cannot leave the origin it first committed;
// file drops and the page's document.title never reach the app's window or
// the page.
type UntrustedOptions struct {
	// MaxMessageBytes is the longest message OnMessage may get, in bytes
	// (default 65536). The web view hands Go the whole message first, so this
	// is no memory bound on that step; the check is the first thing Go does,
	// and a longer message is dropped before it is decoded, queued or given a
	// goroutine, and counted (Page.UntrustedStats). The wrapper of xsecHost
	// also refuses to post one, which only saves well-behaved pages the trip.
	MaxMessageBytes int
	// MaxQueued bounds the messages waiting for OnMessage (default 64). When
	// the queue is full, new ones are dropped and counted: a page flooding
	// the app costs it a counter, not goroutines or memory.
	MaxQueued int
	// OnMessage is called, on a goroutine of its own and one message at a
	// time in the order sent, with the page that sent msg.
	OnMessage func(page *Page, msg string)
	// OnOpenRequest, when set, is told of a page's window.open or
	// target=_blank request with its URL, on the main thread. It is only a
	// notice: mygo opens nothing for an untrusted page, so the app decides
	// (and asks the user, and audits) what to do with the URL. Without it,
	// the requests are dropped and counted.
	OnOpenRequest func(page *Page, url string)
}

// UntrustedStats counts what the app did with the messages of an untrusted page.
type UntrustedStats struct {
	Delivered int64 // handed to OnMessage
	TooLarge  int64 // longer than MaxMessageBytes
	Dropped   int64 // queue full
	Malformed int64 // anything else a page posted (not a message)
	// Blocked counts what the page asked for that mygo refused by default:
	// permissions, downloads, windows, navigations off its origin, and
	// messages from a page that left its origin.
	Blocked int64
}

type untrustedState struct {
	max     int
	onMsg   func(*Page, string)
	queue   chan string
	stats   struct{ delivered, tooLarge, dropped, malformed, blocked atomic.Int64 }
	started atomic.Bool
	pageRef *Window
	onOpen  func(*Page, string)

	mu      sync.Mutex
	closed  bool
	origin  string      // the origin the page first committed
	offSite atomic.Bool // the page committed another origin
}

const (
	defaultUntrustedMaxBytes = 64 << 10
	defaultUntrustedQueue    = 64
)

func newUntrustedState(pw *Window, o *UntrustedOptions) (*untrustedState, error) {
	if o.OnMessage == nil {
		return nil, errors.New("mygo: UntrustedOptions.OnMessage is required")
	}
	u := &untrustedState{max: o.MaxMessageBytes, onMsg: o.OnMessage, pageRef: pw, onOpen: o.OnOpenRequest}
	if u.max <= 0 {
		u.max = defaultUntrustedMaxBytes
	}
	q := o.MaxQueued
	if q <= 0 {
		q = defaultUntrustedQueue
	}
	u.queue = make(chan string, q)
	return u, nil
}

// untrustedHead starts every message the app sends to an untrusted page.
const untrustedHead = `{"t":"u","p":`

func isUntrustedMessage(m message) bool {
	return len(m.head) >= len(untrustedHead) && string(m.head[:len(untrustedHead)]) == untrustedHead
}

// PostUntrusted sends text to the page of a web view made with
// WebViewOptions.Untrusted, where the listeners of xsecHost hear it.
func (p *Page) PostUntrusted(text string) error {
	if p.w.untrusted == nil {
		return errors.New("mygo: the page is not untrusted")
	}
	b, err := json.Marshal(text)
	if err != nil {
		return err
	}
	// Held until the page's DOM is ready, as events are.
	p.w.enqueue(message{head: append([]byte(untrustedHead), b...), tail: []byte("}")}, true)
	return nil
}

// UntrustedStats returns the counts of an untrusted page's messages.
func (p *Page) UntrustedStats() (UntrustedStats, error) {
	u := p.w.untrusted
	if u == nil {
		return UntrustedStats{}, errors.New("mygo: the page is not untrusted")
	}
	return UntrustedStats{u.stats.delivered.Load(), u.stats.tooLarge.Load(), u.stats.dropped.Load(), u.stats.malformed.Load(), u.stats.blocked.Load()}, nil
}

// handleUntrustedMessage handles what the script of an untrusted page posts,
// after the secret: the page's own messages ("U" and the text), and the
// DOM-ready note. It runs on the main thread and does nothing else with the
// text but count it and queue it: no decoding, no goroutine per message.
func (w *Window) handleUntrustedMessage(msg string) {
	u := w.untrusted
	switch {
	case u.offSite.Load():
		// The page left its origin: whatever runs there got the secret with
		// the script, but it is not the page the app made.
		u.stats.blocked.Add(1)
	case msg == `{"t":"dom-ready"}`:
		// Once per document: a commit resets domReady, so a reload or a
		// same-origin navigation gets its own.
		w.outMu.Lock()
		again := w.domReady
		if !again {
			w.domReady = true
			w.outbox = append(w.outbox, w.held...)
			w.held = nil
		}
		w.outMu.Unlock()
		if again {
			u.stats.malformed.Add(1)
			return
		}
		w.flush()
		fire(&w.onDOMReady)
		w.readyToShow()
	case len(msg) > 0 && msg[0] == 'U':
		text := msg[1:]
		if len(text) > u.max {
			u.stats.tooLarge.Add(1)
			return
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		if u.closed {
			return
		}
		if u.started.CompareAndSwap(false, true) {
			go u.run()
		}
		select {
		case u.queue <- text:
		default:
			u.stats.dropped.Add(1)
		}
	default:
		u.stats.malformed.Add(1)
	}
}

// close ends the page's messages: the queue closes, so run returns, and
// later ones are dropped. Main thread (destroyWebView); safe to repeat.
func (u *untrustedState) close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.closed {
		u.closed = true
		u.onOpen = nil
		if !u.started.Load() {
			u.onMsg = nil
		}
		close(u.queue)
	}
}

func (u *untrustedState) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

// run is the one goroutine that calls OnMessage, until the page is
// destroyed. It drops what is still queued then, and lets go of the app's
// closures.
func (u *untrustedState) run() {
	onMsg, pg := u.onMsg, u.pageRef.pg
	defer func() { u.onMsg = nil }()
	for text := range u.queue {
		if u.isClosed() {
			continue
		}
		u.stats.delivered.Add(1)
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("mygo: panic in UntrustedOptions.OnMessage: %v", r)
				}
			}()
			onMsg(pg, text)
		}()
	}
}

// originOf returns the origin of a URL as the untrusted policy compares it.
func originOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// willNavigate is the policy for the main frame of an untrusted page: it
// stays at the origin it first committed. Before that, only the app has
// loaded anything.
func (u *untrustedState) willNavigate(rawURL string) bool {
	u.mu.Lock()
	origin := u.origin
	u.mu.Unlock()
	if origin == "" || originOf(rawURL) == origin {
		return true
	}
	u.stats.blocked.Add(1)
	return false
}

// committed records the origin the page committed: the first is the one it
// is pinned to. It reports whether the page is where the app put it.
func (u *untrustedState) committed(rawURL string) {
	o := originOf(rawURL)
	u.mu.Lock()
	if u.origin == "" {
		u.origin = o
	}
	same := o == u.origin
	u.mu.Unlock()
	u.offSite.Store(!same)
	if !same {
		u.stats.blocked.Add(1)
	}
}

// openRequested handles window.open and target=_blank of an untrusted page:
// nothing opens, the app may be told.
func (u *untrustedState) openRequested(rawURL string) {
	u.stats.blocked.Add(1)
	if u.onOpen != nil {
		u.onOpen(u.pageRef.pg, rawURL)
	}
}

// screenCaptureGuard makes getDisplayMedia reject, in the page and in the
// frames that run this script. The backends turn screen capture off where
// they can (WKPreferences on macOS, the permission request on Linux); this
// is the layer under them, for the web views that have no switch (WebView2)
// and for a switch a system update takes away. It is not a boundary: script
// that gets a pristine MediaDevices from elsewhere is only stopped by those.
const screenCaptureGuard = `(()=>{const deny=function getDisplayMedia(){` +
	`return Promise.reject(new DOMException("Screen capture is not allowed in this page","NotAllowedError"))};` +
	`for(const t of [window.MediaDevices&&MediaDevices.prototype,navigator.mediaDevices]){` +
	`if(t)try{Object.defineProperty(t,"getDisplayMedia",{value:deny,writable:false,configurable:false})}catch(e){}}})();`

// untrustedScript is the page's only connection to the app. It binds the
// native postMessage at document start and defines objects the page cannot
// replace, so the page cannot be handed the secret by swapping postMessage.
// The secret only tells the app's own script's messages from other noise
// posted to the same handler; it is not a boundary (see UntrustedOptions):
// whoever has it can only send what xsecHost sends.
func untrustedScript(secret string) string {
	s, _ := json.Marshal(secret)
	return screenCaptureGuard + `(()=>{if(window.xsecHost)return;const h=window.webkit&&window.webkit.messageHandlers&&window.webkit.messageHandlers.mygo;` +
		`const c=window.chrome&&window.chrome.webview;` +
		// Bound now, at document start: the page cannot swap postMessage later
		// to be handed what is posted.
		`const hp=h&&h.postMessage.bind(h),cp=c&&c.postMessage.bind(c);const post=hp?(m)=>hp(m):cp?(m)=>cp(m):null;if(!post)return;` +
		`const S=` + string(s) + `;const ls=new Set();` +
		`const receive=(a)=>{for(const m of a){if(m&&m.t==="u"&&typeof m.p==="string")for(const f of [...ls]){try{f(m.p)}catch(e){setTimeout(()=>{throw e})}}}};` +
		`Object.defineProperty(window,"__mygo",{value:Object.freeze({receive}),enumerable:false});` +
		`Object.defineProperty(window,"xsecHost",{value:Object.freeze({` +
		`postMessage:(t)=>{if(typeof t!=="string")throw new TypeError("xsecHost.postMessage takes a string");post(S+"U"+t)},` +
		`addListener:(f)=>{if(typeof f!=="function")throw new TypeError("listener must be a function");ls.add(f);return()=>ls.delete(f)}}),enumerable:true});` +
		`const ready=()=>{try{post(S+'{"t":"dom-ready"}')}catch(e){}};` +
		`document.readyState==="loading"?document.addEventListener("DOMContentLoaded",ready,{once:true}):ready();})();`
}
