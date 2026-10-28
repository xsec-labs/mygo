package ui

import (
	gocontext "context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// mainThread runs functions one at a time on a goroutine of its own, as a
// window's main thread does.
type mainThread struct{ q chan func() }

func newMainThread(t *testing.T) *mainThread {
	m := &mainThread{q: make(chan func(), 256)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range m.q {
			f()
		}
	}()
	t.Cleanup(func() { close(m.q); <-done })
	return m
}

func (m *mainThread) post(f func()) { m.q <- f }

// automatedSurface draws a frame on the main thread whenever one is asked
// for, as a backend's display link does.
type automatedSurface struct {
	testSurface
	main *mainThread
	conn *surface.Conn
}

func (s *automatedSurface) Size() (float64, float64, float64) { return 400, 300, 1 }
func (s *automatedSurface) RequestFrame() {
	s.main.post(func() {
		if s.conn.Event != nil {
			s.conn.Event(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
		}
	})
}

type automatedApp struct {
	count int
	name  string
	agree bool
}

func (a *automatedApp) view(c *context) {
	coreColumn(c).Fill().Padding(10).Gap(8).Children(func() {
		if coreButton(c, "Increment").Clicked() {
			a.count++
		}
		coreTextf(c, "Count: %d", a.count)
		coreTextInput(c, &a.name).Placeholder("Your name").Label("Name")
		coreCheckbox(c, &a.agree, "I agree")
	})
}

// openAutomated attaches the app to a fake window and returns its
// Automation and the connection.
func openAutomated(t *testing.T, app *automatedApp) (*Automation, *surface.Conn) {
	t.Helper()
	t.Setenv("MYGO_GPU", "0")
	m := newMainThread(t)
	s := &automatedSurface{main: m}
	window := &struct{ name string }{"window"}
	conn := &surface.Conn{Surface: s, Window: window, Post: m.post, Invalidate: s.RequestFrame}
	s.conn = conn
	attached := make(chan struct{})
	m.post(func() {
		coreView(app.view).AttachContent(conn)
		close(attached)
	})
	<-attached
	a, ok := Automate(window)
	if !ok {
		t.Fatal("a window showing a View is not automated")
	}
	t.Cleanup(func() {
		m.post(func() {
			if conn.Detach != nil && !a.h.detached {
				conn.Detach()
			}
		})
	})
	return a, conn
}

func within(t *testing.T) gocontext.Context {
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (s AutomationSnapshot) find(role, label string) (AutomationNode, bool) {
	for _, n := range s.Nodes {
		if n.Role == role && (n.Label == label || n.Value == label) {
			return n, true
		}
	}
	return AutomationNode{}, false
}

func (s AutomationSnapshot) has(text string) bool {
	return slices.ContainsFunc(s.Nodes, func(n AutomationNode) bool { return n.Label == text || n.Value == text })
}

func nodeCenter(n AutomationNode) (float64, float64) {
	return float64(n.Bounds.X + n.Bounds.W/2), float64(n.Bounds.Y + n.Bounds.H/2)
}

func TestAutomationClicksWhatTheTreeShows(t *testing.T) {
	app := &automatedApp{}
	a, _ := openAutomated(t, app)
	ctx := within(t)

	s, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("Count: 0") })
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 400 || s.Height != 300 || s.Frame == 0 {
		t.Errorf("snapshot %vx%v after %d frames", s.Width, s.Height, s.Frame)
	}
	button, ok := s.find("button", "Increment")
	if !ok {
		t.Fatalf("no Increment button in %+v", s.Nodes)
	}
	if !slices.Contains(button.States, "focusable") || !slices.Contains(button.Actions, "press") {
		t.Errorf("button states %v actions %v", button.States, button.Actions)
	}
	x, y := nodeCenter(button)
	if err := a.Inject(ctx,
		AutomationInput{Kind: "move", X: x, Y: y},
		AutomationInput{Kind: "down", X: x, Y: y},
		AutomationInput{Kind: "up", X: x, Y: y},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("Count: 1") }); err != nil {
		t.Fatalf("after a click: %v", err)
	}

	// The accessibility action presses without the pointer.
	if err := a.Inject(ctx, AutomationInput{Kind: "access", ID: button.ID, Action: "press"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("Count: 2") }); err != nil {
		t.Fatalf("after a press: %v", err)
	}
}

func TestAutomationTypesChineseThroughTheFocusedInput(t *testing.T) {
	app := &automatedApp{}
	a, _ := openAutomated(t, app)
	ctx := within(t)

	s, err := a.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	field, ok := s.find("textfield", "Name")
	if !ok {
		t.Fatalf("no Name field in %+v", s.Nodes)
	}
	if field.Placeholder != "Your name" {
		t.Errorf("placeholder %q", field.Placeholder)
	}
	x, y := nodeCenter(field)
	if err := a.Inject(ctx, AutomationInput{Kind: "down", X: x, Y: y}, AutomationInput{Kind: "up", X: x, Y: y}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.Focus == field.ID }); err != nil {
		t.Fatalf("the click did not focus the field: %v", err)
	}
	if err := a.Inject(ctx,
		AutomationInput{Kind: "compose", Text: "shen tou", Caret: 8},
		AutomationInput{Kind: "compose"},
		AutomationInput{Kind: "text", Text: "渗透测试报告"},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("渗透测试报告") }); err != nil {
		t.Fatalf("typed text: %v (app has %q)", err, app.name)
	}
	if app.name != "渗透测试报告" {
		t.Errorf("the app's value is %q", app.name)
	}
}

func TestAutomationKeysReachTheFocusedElement(t *testing.T) {
	app := &automatedApp{}
	a, _ := openAutomated(t, app)
	ctx := within(t)

	s, _ := a.Snapshot(ctx)
	box, ok := s.find("checkbox", "I agree")
	if !ok {
		t.Fatalf("no checkbox in %+v", s.Nodes)
	}
	if err := a.Inject(ctx, AutomationInput{Kind: "access", ID: box.ID, Action: "focus"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Inject(ctx, AutomationInput{Kind: "keydown", Key: KeySpace}, AutomationInput{Kind: "keyup", Key: KeySpace}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool {
		n, _ := s.Node(box.ID)
		return slices.Contains(n.States, "checked")
	}); err != nil {
		t.Fatalf("space did not check the box (agree=%v): %v", app.agree, err)
	}
}

func TestAutomationWaitEndsWithTheContext(t *testing.T) {
	a, _ := openAutomated(t, &automatedApp{})
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("never shown") })
	if !errors.Is(err, gocontext.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
}

func TestAutomationCallerThatGaveUpLeavesNoWatchBehind(t *testing.T) {
	a, conn := openAutomated(t, &automatedApp{})
	gate := make(chan struct{})
	conn.Post(func() { <-gate }) // the main thread is busy until the caller has given up
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 50*time.Millisecond)
	defer cancel()
	var calls atomic.Int32
	_, err := a.Wait(ctx, func(AutomationSnapshot) bool { calls.Add(1); return false })
	if !errors.Is(err, gocontext.DeadlineExceeded) {
		t.Fatalf("got %v, want the deadline", err)
	}
	close(gate)
	flushed := make(chan struct{})
	conn.Post(func() { close(flushed) })
	<-flushed
	if n := calls.Load(); n != 0 {
		t.Fatalf("the condition of a Wait that had ended ran %d times", n)
	}
	var watching int
	ran := make(chan struct{})
	conn.Post(func() { watching = len(a.h.watches); close(ran) })
	<-ran
	if watching != 0 {
		t.Fatalf("%d watches left behind", watching)
	}
}

func TestAutomationEndsWhenTheWindowCloses(t *testing.T) {
	a, conn := openAutomated(t, &automatedApp{})
	ctx := within(t)
	waited := make(chan error, 1)
	go func() {
		_, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.has("never shown") })
		waited <- err
	}()
	if _, err := a.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	a.h.conn.Post(conn.Detach)
	select {
	case err := <-waited:
		if !errors.Is(err, ErrAutomationClosed) {
			t.Fatalf("got %v, want ErrAutomationClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait outlived the window")
	}
	if _, err := a.Snapshot(ctx); !errors.Is(err, ErrAutomationClosed) {
		t.Fatalf("a snapshot of a closed window: %v", err)
	}
	if _, ok := Automate(a.h.conn.Window); ok {
		t.Error("a closed window is still automated")
	}
}

func TestAutomationRejectsUnknownInput(t *testing.T) {
	a, _ := openAutomated(t, &automatedApp{})
	ctx := within(t)
	if err := a.Inject(ctx, AutomationInput{Kind: "teleport"}); err == nil || !strings.Contains(err.Error(), "teleport") {
		t.Errorf("unknown kind: %v", err)
	}
	if err := a.Inject(ctx, AutomationInput{Kind: "access", ID: 1, Action: "levitate"}); err == nil || !strings.Contains(err.Error(), "levitate") {
		t.Errorf("unknown action: %v", err)
	}
}

func TestAutomateIgnoresWindowsWithoutAView(t *testing.T) {
	if _, ok := Automate(nil); ok {
		t.Error("nil window")
	}
	if _, ok := Automate(&struct{}{}); ok {
		t.Error("unknown window")
	}
}

func TestParseKey(t *testing.T) {
	for name, want := range map[string]Key{
		"Enter": KeyEnter, "ESCAPE": KeyEscape, " tab ": KeyTab, "space": KeySpace, "PageDown": KeyPageDown,
		"a": KeyA, "Z": KeyZ, "0": Key0, "9": Key9, "F1": KeyF1, "f12": KeyF12, "Left": KeyLeft,
	} {
		if got, ok := ParseKey(name); !ok || got != want {
			t.Errorf("ParseKey(%q) = %v, %v; want %v", name, got, ok, want)
		}
	}
	for _, name := range []string{"", "F13", "F0", "F01", "ab", "Hyper", "!"} {
		if k, ok := ParseKey(name); ok {
			t.Errorf("ParseKey(%q) = %v, want none", name, k)
		}
	}
}

func TestAutomationNamesEveryRole(t *testing.T) {
	if got, want := len(accessRoleNames), int(platform.RoleHeading)+1; got != want {
		t.Fatalf("%d role names for %d roles", got, want)
	}
	seen := map[string]bool{}
	for _, n := range accessRoleNames {
		if n == "" || seen[n] {
			t.Errorf("role name %q is empty or repeated", n)
		}
		seen[n] = true
	}
	checks := map[platform.AccessRole]string{platform.RoleButton: "button", platform.RoleTextField: "textfield", platform.RoleHeading: "heading", platform.RoleMenuItemRadio: "menuitemradio"}
	for r, want := range checks {
		if got := accessRoleName(r); got != want {
			t.Errorf("role %d is %q, want %q", r, got, want)
		}
	}
}
