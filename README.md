<p align="center">
<img width="800" height="360" alt="MyGo!!!!!" src="https://github.com/user-attachments/assets/c3eee9bc-5f22-4032-95b6-776e8381910a" />
</p>

**Desktop apps in Go, with native UI or a web frontend.**

Sponsored by:

<p align="center">
  <a href="https://openuptime.app/?utm_source=github&utm_medium=sponsor&utm_campaign=mygo">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://openuptime.app/sponsor/banner-dark.png">
      <img width="600" alt="OpenUptime: open-source uptime monitoring and status pages" src="https://openuptime.app/sponsor/banner-light.png">
    </picture>
  </a>
</p>


## xsec-labs fork

This is [xsec-labs/mygo](https://github.com/xsec-labs/mygo), a fork of
[egoist/mygo](https://github.com/egoist/mygo) for xSecDesktop. The branch
`xsec/main` is upstream commit `9c69cf829ece` (`fix(terminal): ignore momentum
scrolling for zoom (#709)`) plus the two changes below, and nothing else.
xSecDesktop pins a commit of `xsec/main` through a `replace` directive. Nothing
here has been sent upstream yet.

| Change | Source | Upstream |
| --- | --- | --- |
| `WebViewOptions.Untrusted`: web views for pages the app does not trust | xSecDesktop task T-02, commit `cf7ecd0` | issue [egoist/mygo#729](https://github.com/egoist/mygo/issues/729) |
| `markdown/parser`: `Document.Set` keeps no copy per append | xSecDesktop task T-01b, commit `607aad7` | none filed |

### Untrusted web views (T-02)

*Motivation.* Pages served by a custom scheme are the app's own and fully
trusted: they can call every bound method, use `mygo:window.*` (including
`Close`), receive every broadcast event, send messages of any size, and get
permission requests, `window.open`, downloads and main-frame navigation
handled by default. xSecDesktop hosts third-party plugin UIs in a
`ui.WebView`, and "bind nothing" does not cover most of that.

*Change.* `WebViewOptions.Untrusted` (nil keeps today's behaviour) gives the
page one script and no bridge: no `window.mygo`, no bound methods, no
`mygo:window.*`, no events, preferences or locale. Page and app exchange text
through `xsecHost.postMessage` and `xsecHost.addListener`; the app receives
the sending `*Page` in `UntrustedOptions.OnMessage`. Messages are size-limited
and queued with a bound. Permission requests, `window.open` and
`target=_blank` (reported to `OnOpenRequest`), downloads, main-frame
navigation off the first committed origin, script clipboard access and element
full screen are refused unless the app opts in. On Windows, custom scheme URLs
map per host to `http://<host>.<scheme>.localhost`. New files: `untrusted.go`,
`untrusted_test.go`, `internal/windows/scheme_url_test.go`.

*Limits.* Verified on Linux (WebKitGTK) only; the Windows host mapping and
macOS are not run. The size limits cannot stop the web engine from delivering
the whole string to the process first.

### Document.Set without a copy per append (T-01b)

*Motivation.* An app streaming a reply often builds it as `text += token`, a
new string each time, and hands it to `markdown.Stream`. `Document.Set` kept
the string it was given, and the blocks parsed earlier kept the older strings,
so every append kept a copy of the text alive. In xSecDesktop's T-01b stress
test a 390 KB reply held 1.5 GB of heap.

*Change.* `Set` appends a new string to the document's own buffer, which grows
by doubling; more of the same string (`src[:n]` of a growing `n`) is still
copied nowhere. Files: `ui/markdown/parser/document.go`,
`ui/markdown/parser/stream_test.go`.

### Maintaining the fork

`xsec/main` is rebased or merged onto upstream deliberately, one pinned commit
at a time. Both changes were taken from branches `claude/project-thread-86q06n`
and `claude/project-thread-8a0eub`, which were built on a newer upstream main
(`c1c7a77`); only their own commits were cherry-picked, so no unrelated
upstream work comes along. Before moving the pin, run `go build`, `go vet` for
darwin, linux and windows, the `js/wasm` build and tests listed in
`.github/workflows/ci.yml`, and `go test -race`. On the baseline `9c69cf8`,
`go test -race` already fails `TestEvalLateReply` (a data race in
`Page.EvalContext`) and, in `ui`, `TestDiffFrameAllocs` (its allocation counts
assume no race detector); both fail the same way without our changes.

---

Each MyGo window shows one of two kinds of interface, and one app, or one
window, can mix them:

- **A web page**, in the webview the OS already has: WKWebView on macOS,
  WebKitGTK on Linux, WebView2 on Windows. Build the frontend with any web
  tools; it calls your Go code through a TypeScript client generated from it.
- **Native UI**, written in Go alone with package `ui` and
  drawn by MyGo itself on the GPU: no HTML, no JavaScript and no webview, so
  the window opens at once and takes little memory.

Either way, an app is a single Go binary of a few megabytes, focused on low
memory and CPU use.

- **Pure Go, no cgo**: build for every platform from any machine.
- **Typed IPC** for pages: bind Go services, stream values through channels
  and send typed events; the TypeScript client is generated from your Go
  code.
- **A Go UI toolkit** for native UI: flexbox and grid layout, widgets, text
  editing with input methods, virtualized lists, SVG icons, animations,
  screen reader support, and views you test without a window; Liquid Glass
  on every platform with the glass plugin; web pages inside native UI, under
  the menus and dialogs it shows over them.
- **Desktop APIs** for both: windows, menus, tray, dialogs, notifications,
  global shortcuts, deep links, file associations, recent documents, the
  system accent color and accessibility settings, watching files, and more.
- **Ready to ship**: app bundles and disk images, Windows installers, Debian
  packages and a Linux install script, code signing, notarization, and signed
  auto-updates with delta updates and an update window in the manner of
  Sparkle.

A window with a web page, whose frontend calls Go:

```go
type Greeter struct{}

// Greet returns a greeting.
func (Greeter) Greet(name string) string { return "Hello, " + name }

func main() {
	mygo.Bind(Greeter{})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Hello", URL: "/"})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

```ts
import { Greeter } from "./mygo"; // generated

document.body.textContent = await Greeter.greet("Ada");
```

A window with native UI, whose view is a Go function of the app's state:

```go
type counter struct{ n int }

func (s *counter) view(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Text(c, fmt.Sprint(s.n)).FontSize(40).Bold()
		if ui.PrimaryButton(c, "Increment").Clicked() {
			s.n++
		}
	})
}

func main() {
	s := &counter{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Counter",
			Content: ui.View(s.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

## Getting started

With [Go](https://go.dev/dl/) 1.27.3+ and [Bun](https://bun.sh), create an app
with a web frontend:

```sh
bunx mygo-cli init my-app      # or: npx mygo-cli init my-app
cd my-app
bun run dev
```

An app of native UI needs Go alone:

```sh
go run github.com/egoist/mygo/cmd/mygo@latest init -template native my-app
cd my-app
go tool mygo dev
```

Read the [documentation](docs/README.md).

## License

MIT
