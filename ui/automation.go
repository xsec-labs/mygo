package ui

import (
	gocontext "context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Automation drives a live window that shows a View from outside its view
// function, as a user and a screen reader would: it reads the elements the
// last frame shows, sends the window the events its surface sends (pointer,
// keys, text, an input method's composition, the actions of assistive
// technology) and waits for what the frames show, with no timers: a wait
// ends when a frame presented what it waits for.
//
// It exists for tools that operate a running app, such as the UI
// automation server of a development build. All methods are safe from any
// goroutine; they run their work on the main thread and wait for it.
type Automation struct{ h *windowHost }

// ErrAutomationClosed is returned once the window has closed.
var ErrAutomationClosed = errors.New("mygo: the window has closed")

// automated holds the windows showing a View, by their *mygo.Window. A
// list rather than a map: a window or two, and it costs apps no code.
var (
	automatedMu sync.Mutex
	automated   []automatedWindow
)

type automatedWindow struct {
	window any
	host   *windowHost
}

func automate(window any, h *windowHost) {
	automatedMu.Lock()
	defer automatedMu.Unlock()
	automated = append(automated, automatedWindow{window, h})
}

func unautomate(window any) {
	automatedMu.Lock()
	defer automatedMu.Unlock()
	automated = slices.DeleteFunc(automated, func(w automatedWindow) bool { return w.window == window })
}

// Automate returns the Automation of window, the *mygo.Window showing a
// View, and false for any other window.
func Automate(window any) (*Automation, bool) {
	if window == nil {
		return nil, false
	}
	automatedMu.Lock()
	defer automatedMu.Unlock()
	for _, w := range automated {
		if w.window == window {
			return &Automation{h: w.host}, true
		}
	}
	return nil, false
}

// AutomatedWindows lists the open windows that show a View, as the
// *mygo.Window values they were made with.
func AutomatedWindows() []any {
	automatedMu.Lock()
	defer automatedMu.Unlock()
	var out []any
	for _, w := range automated {
		out = append(out, w.window)
	}
	return out
}

// AutomationNode is an element of a window's frame as assistive technology
// sees it: the widgets, the texts, and the elements that take the focus or
// have a Label.
type AutomationNode struct {
	// ID identifies the element from frame to frame.
	ID uint64
	// Parent is the index of the parent in AutomationSnapshot.Nodes, -1
	// for the top.
	Parent int
	// Role is what the element is: "button", "text", "textfield", "list",
	// and so on.
	Role string
	// Label names the element, Value is its value (a text field's text),
	// Placeholder what an empty field shows and Description its help text
	// or error.
	Label, Value, Placeholder, Description string
	// Bounds is the element's visible box in DIPs, relative to the window's
	// content.
	Bounds Rect
	// States lists "focusable", "disabled", "checked", "mixed",
	// "selected", "expanded", "offscreen", and the like.
	States []string
	// Actions lists what AutomationInput{Kind: "access"} does to it:
	// "press", "focus", "increment", "decrement", "setvalue",
	// "scrollintoview", "expand".
	Actions []string
	// Level is the depth of a tree item or the rank of a heading, PosInSet
	// and SetSize the place of an item in its set; all 0 when unknown.
	Level, PosInSet, SetSize int
	// Range is {min, max, now} for sliders, progress bars, meters and
	// steppers, nil for other elements.
	Range []float64
}

// AutomationSnapshot is the content of a window as of a presented frame.
type AutomationSnapshot struct {
	// Frame counts the frames the window presented; it grows with each.
	Frame uint64
	// Width, Height and Scale are the content's size in DIPs and its pixel
	// ratio.
	Width, Height, Scale float64
	// Focus is the ID of the element with the keyboard focus, 0 for none.
	Focus uint64
	Nodes []AutomationNode
	// Announcements are the texts the frame asked screen readers to read.
	// The window clears them when the frame ends, so only a snapshot a Wait
	// condition sees (or returns) carries them; Snapshot, taken between
	// frames, finds none.
	Announcements []string
}

// Node returns the node with id.
func (s AutomationSnapshot) Node(id uint64) (AutomationNode, bool) {
	for _, n := range s.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return AutomationNode{}, false
}

// AutomationInput is one event for the window. Kind is one of:
//
//	"move"      the pointer goes to X, Y
//	"down"      Button (0 primary, 1 secondary, 2 middle) goes down at X, Y;
//	            Clicks counts quick presses (1 when 0)
//	"up"        Button goes up at X, Y
//	"scroll"    scrolls by DX, DY DIPs at X, Y, as a touchpad does
//	"keydown"   Key goes down with Mods held; "keyup" lets it go
//	"text"      Text is typed, or committed by an input method
//	"compose"   Text is an input method's composition with its caret at
//	            rune Caret; an empty Text ends it
//	"command"   an edit command of the menus in Text, as "copy" or "selectAll"
//	"access"    Action ("press", "focus", "increment", "decrement",
//	            "expand", "collapse", "scrollintoview", "setvalue",
//	            "inserttext", "selecttext") on element ID, with Text its value
//	"focus"     the window gets the keyboard; "blur" loses it
type AutomationInput struct {
	Kind     string
	X, Y     float64
	DX, DY   float64
	Button   int
	Clicks   int
	Key      Key
	Mods     Modifiers
	Text     string
	Caret    int
	ID       uint64
	Action   string
	From, To int
}

// Snapshot returns what the window's last frame shows. A window out of
// sight builds the frame it held back first.
func (a *Automation) Snapshot(ctx gocontext.Context) (AutomationSnapshot, error) {
	var s AutomationSnapshot
	err := a.do(ctx, func() error {
		a.h.fresh()
		s = a.h.snapshot()
		return nil
	})
	return s, err
}

// Wait returns the first snapshot cond accepts: the current one if it
// does, else the first frame presented after that it does. cond runs on the
// main thread and must be quick and pure. Wait ends with ctx's error when
// the context ends first, and ErrAutomationClosed when the window closes.
func (a *Automation) Wait(ctx gocontext.Context, cond func(AutomationSnapshot) bool) (AutomationSnapshot, error) {
	got := make(chan AutomationSnapshot, 1)
	var off func()
	err := a.do(ctx, func() error {
		a.h.fresh()
		if s := a.h.snapshot(); cond(s) {
			got <- s
			return nil
		}
		off = a.h.watch(func(s AutomationSnapshot) bool {
			if !cond(s) {
				return false
			}
			got <- s
			return true
		})
		return nil
	})
	if err != nil {
		return AutomationSnapshot{}, err
	}
	select {
	case s := <-got:
		return s, nil
	case <-ctx.Done():
		if off != nil {
			a.h.post(off)
		}
		return AutomationSnapshot{}, ctx.Err()
	case <-a.h.closedCh():
		return AutomationSnapshot{}, ErrAutomationClosed
	}
}

// Inject sends the events one after another, as a window's surface does.
// After each it waits for the frame the event asked for, so that the next
// finds what the last changed (a press must be built before its release
// clicks), as a Tester settles. A window that does not present the frame
// ends the wait with ctx's error.
func (a *Automation) Inject(ctx gocontext.Context, in ...AutomationInput) error {
	for _, i := range in {
		ev, err := i.surfaceEvent()
		if err != nil {
			return err
		}
		var from uint64
		var asked bool
		if err := a.do(ctx, func() error {
			from = a.h.frames
			a.h.pending.Store(false)
			a.h.event(ev)
			asked = a.h.pending.Load()
			return nil
		}); err != nil {
			return err
		}
		if !asked {
			continue
		}
		if _, err := a.Wait(ctx, func(s AutomationSnapshot) bool { return s.Frame > from }); err != nil {
			return fmt.Errorf("mygo: no frame followed the %s event: %w", i.Kind, err)
		}
	}
	return nil
}

// do runs fn on the main thread and returns its error.
func (a *Automation) do(ctx gocontext.Context, fn func() error) error {
	if a.h.conn.Post == nil {
		return errors.New("mygo: the window cannot run automation")
	}
	done := make(chan error, 1)
	a.h.conn.Post(func() {
		if a.h.detached {
			done <- ErrAutomationClosed
			return
		}
		// A caller that gave up before the main thread got here must not leave a watch behind or
		// have its event injected after it was told the call failed.
		if err := ctx.Err(); err != nil {
			done <- err
			return
		}
		done <- fn()
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-a.h.closedCh():
		return ErrAutomationClosed
	}
}

func (i AutomationInput) surfaceEvent() (platform.SurfaceEvent, error) {
	ev := platform.SurfaceEvent{X: i.X, Y: i.Y, DX: i.DX, DY: i.DY, Button: i.Button, Clicks: i.Clicks,
		Key: platform.Key(i.Key), Mods: platform.Modifiers(i.Mods), Text: i.Text, Caret: i.Caret, ID: i.ID, From: i.From, To: i.To}
	switch i.Kind {
	case "move":
		ev.Kind = platform.PointerMove
	case "down":
		ev.Kind = platform.PointerDown
		if ev.Clicks == 0 {
			ev.Clicks = 1
		}
	case "up":
		ev.Kind = platform.PointerUp
	case "scroll":
		ev.Kind, ev.Precise = platform.PointerScroll, true
	case "keydown":
		ev.Kind = platform.KeyPressed
	case "keyup":
		ev.Kind = platform.KeyReleased
	case "text":
		ev.Kind = platform.TextInput
	case "compose":
		ev.Kind = platform.TextComposition
	case "command":
		ev.Kind = platform.SurfaceCommand
	case "focus":
		ev.Kind = platform.SurfaceFocus
	case "blur":
		ev.Kind = platform.SurfaceBlur
	case "access":
		ev.Kind = platform.AccessAction
		act, ok := accessActionKinds[strings.ToLower(i.Action)]
		if !ok {
			return ev, fmt.Errorf("mygo: unknown accessibility action %q", i.Action)
		}
		ev.Action = act
	default:
		return ev, fmt.Errorf("mygo: unknown automation input %q", i.Kind)
	}
	return ev, nil
}

var accessActionKinds = map[string]platform.AccessActionKind{
	"press": platform.AccessPress, "focus": platform.AccessFocus, "increment": platform.AccessIncrement,
	"decrement": platform.AccessDecrement, "setvalue": platform.AccessSetValue, "scrollintoview": platform.AccessScrollIntoView,
	"expand": platform.AccessExpand, "collapse": platform.AccessCollapse, "selecttext": platform.AccessSelectText,
	"inserttext": platform.AccessInsertText,
}

// automationWatch is a Wait waiting for a frame: run reports whether it is
// done. It holds the code that reads the frame, so that only apps that
// automate their windows link it.
type automationWatch struct {
	run func() bool
}

// watch has cond run on each frame presented from now on, until it returns
// true or the returned function is called. Main thread only.
func (h *windowHost) watch(cond func(AutomationSnapshot) bool) (off func()) {
	w := &automationWatch{run: func() bool { return cond(h.snapshot()) }}
	h.watches = append(h.watches, w)
	return func() {
		for i, x := range h.watches {
			if x == w {
				h.watches = append(h.watches[:i], h.watches[i+1:]...)
				return
			}
		}
	}
}

// notify tells the waits of a presented frame.
func (h *windowHost) notify() {
	h.frames++
	if len(h.watches) == 0 {
		return
	}
	keep := h.watches[:0]
	for _, w := range h.watches {
		if !w.run() {
			keep = append(keep, w)
		}
	}
	clear(h.watches[len(keep):])
	h.watches = keep
}

// fresh builds the frame a window out of sight held back, so that a
// snapshot shows what the app changed.
func (h *windowHost) fresh() {
	if h.last == nil || h.rt.held {
		h.rt.runFrame()
	}
}

// closedCh returns a channel that is closed once the window has.
func (h *windowHost) closedCh() <-chan struct{} {
	h.closeOnce.Do(func() { h.closed = make(chan struct{}) })
	return h.closed
}

func (h *windowHost) isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// snapshot describes the last frame.
func (h *windowHost) snapshot() AutomationSnapshot {
	w, hh, scale := h.rt.host.size()
	tree := h.rt.accessTree()
	s := AutomationSnapshot{Frame: h.frames, Width: float64(w), Height: float64(hh), Scale: float64(scale),
		Focus: tree.Focus, Announcements: tree.Announcements, Nodes: make([]AutomationNode, len(tree.Nodes))}
	for i, n := range tree.Nodes {
		s.Nodes[i] = AutomationNode{
			ID: n.ID, Parent: n.Parent, Role: accessRoleName(n.Role), Label: n.Label, Value: n.Value,
			Placeholder: n.Placeholder, Description: n.Description,
			Bounds: Rect{float32(n.Bounds.X), float32(n.Bounds.Y), float32(n.Bounds.W), float32(n.Bounds.H)},
			States: accessStateNames(n.States), Actions: accessActionNames(n.Actions),
			Level: n.Level, PosInSet: n.PosInSet, SetSize: n.SetSize,
		}
		if n.Role.Ranged() {
			s.Nodes[i].Range = []float64{n.Min, n.Max, n.Now}
		}
	}
	return s
}

var accessRoleNames = [...]string{
	"group", "text", "button", "link", "checkbox", "radio", "switch", "slider", "progress", "textfield", "image", "list", "scroll",
	"dialog", "popup", "tooltip", "popupbutton", "tablist", "tab", "splitter", "status", "table", "row", "cell", "columnheader",
	"tree", "treeitem", "listitem", "menubutton", "toolbar", "radiogroup", "togglebutton", "combobox", "disclosure", "meter",
	"stepper", "colorwell", "alertdialog", "menu", "menubar", "menuitem", "menuitemcheckbox", "menuitemradio", "heading",
}

func accessRoleName(r platform.AccessRole) string {
	if int(r) < len(accessRoleNames) {
		return accessRoleNames[r]
	}
	return fmt.Sprintf("role%d", r)
}

var accessStateNameList = []struct {
	bit  platform.AccessStates
	name string
}{
	{platform.AccessFocusable, "focusable"}, {platform.AccessDisabled, "disabled"}, {platform.AccessChecked, "checked"},
	{platform.AccessMixed, "mixed"}, {platform.AccessSelected, "selected"}, {platform.AccessExpanded, "expanded"},
	{platform.AccessMultiline, "multiline"}, {platform.AccessPassword, "password"}, {platform.AccessReadOnly, "readonly"},
	{platform.AccessSelectable, "selectable"}, {platform.AccessOffscreen, "offscreen"},
	{platform.AccessMultiselectable, "multiselectable"}, {platform.AccessSegment, "segment"}, {platform.AccessSearch, "search"},
	{platform.AccessInvalid, "invalid"}, {platform.AccessSortAscending, "sortascending"},
	{platform.AccessSortDescending, "sortdescending"}, {platform.AccessExpandable, "expandable"}, {platform.AccessVertical, "vertical"},
}

func accessStateNames(s platform.AccessStates) []string {
	var out []string
	for _, x := range accessStateNameList {
		if s&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return out
}

func accessActionNames(a platform.AccessActions) []string {
	var out []string
	for _, x := range []struct {
		bit  platform.AccessActions
		name string
	}{
		{platform.ActionPress, "press"}, {platform.ActionFocus, "focus"}, {platform.ActionIncrement, "increment"},
		{platform.ActionDecrement, "decrement"}, {platform.ActionSetValue, "setvalue"},
		{platform.ActionScrollIntoView, "scrollintoview"}, {platform.ActionExpand, "expand"},
	} {
		if a&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return out
}

// automationKeys names the keys ParseKey knows, in lower case.
var automationKeys = map[string]Key{
	"enter": KeyEnter, "return": KeyEnter, "escape": KeyEscape, "esc": KeyEscape, "backspace": KeyBackspace, "tab": KeyTab,
	"space": KeySpace, "delete": KeyDelete, "insert": KeyInsert, "home": KeyHome, "end": KeyEnd, "pageup": KeyPageUp,
	"pagedown": KeyPageDown, "left": KeyLeft, "right": KeyRight, "up": KeyUp, "down": KeyDown,
	"minus": KeyMinus, "equal": KeyEqual, "comma": KeyComma, "period": KeyPeriod, "slash": KeySlash, "semicolon": KeySemicolon,
	"quote": KeyQuote, "bracketleft": KeyBracketLeft, "bracketright": KeyBracketRight, "backslash": KeyBackslash,
	"backquote": KeyBackquote, "contextmenu": KeyContextMenu,
}

// ParseKey returns the key named name: "Enter", "Escape", "Tab", "Space",
// "Backspace", "Delete", "Left", "PageDown", "F1" to "F12", "a" to "z" and
// "0" to "9", in any case.
func ParseKey(name string) (Key, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if k, ok := automationKeys[n]; ok {
		return k, true
	}
	if len(n) == 1 {
		switch c := n[0]; {
		case c >= 'a' && c <= 'z':
			return KeyA + Key(c-'a'), true
		case c >= '0' && c <= '9':
			return Key0 + Key(c-'0'), true
		}
	}
	if len(n) >= 2 && n[0] == 'f' {
		var f int
		if _, err := fmt.Sscanf(n[1:], "%d", &f); err == nil && f >= 1 && f <= 12 && fmt.Sprint(f) == n[1:] {
			return KeyF1 + Key(f-1), true
		}
	}
	return KeyUnknown, false
}
