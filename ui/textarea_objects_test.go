package ui

import (
	"testing"
)

// objectText is "ab", an inline object (the private-use rune U+E000) and
// "cd".
const objectText = "abcd"

type objectApp struct {
	text    string
	painted []InputObject
	pressed []InputObject
}

func (a *objectApp) build(c *context) {
	coreTextAreaBase(c, &a.text).Width(300).Height(80).
		TextRanges(TextRange{Start: 2, End: 3, ObjectWidth: 40, ObjectHeight: 16, ObjectText: "[file]"}).
		OnTextObjects(func(p *Painter, o InputObject) {
			a.painted = append(a.painted, o)
			p.Fill(o.Rect, RGB(200, 200, 255), 4)
		}).
		OnTextObjectPress(func(o InputObject) { a.pressed = append(a.pressed, o) })
}

func objectEditor(tt *Tester) *editor {
	for _, st := range tt.rt.states {
		if st.editor != nil {
			return st.editor
		}
	}
	return nil
}

// An inline object takes the width and height it was given, is painted
// where the text puts it, and moves the runes after it by its width.
func TestTextAreaObjectBox(t *testing.T) {
	app := &objectApp{text: objectText}
	tt := coreNewTester(app.build, 320, 120)
	if len(app.painted) == 0 {
		t.Fatal("the object was not painted")
	}
	o := app.painted[len(app.painted)-1]
	if o.Index != 2 || o.Rect.W != 40 || o.Rect.H != 16 {
		t.Fatalf("object painted as %+v, want rune 2 in a 40x16 box", o)
	}
	ed := objectEditor(tt)
	x0, _, _ := ed.area.caretAt(ed, 2, 0)
	x1, _, _ := ed.area.caretAt(ed, 3, 0)
	if got := x1 - x0; got != 40 {
		t.Errorf("the object is %v wide in the text, want 40", got)
	}
}

// The caret, Backspace and Delete take an object as one rune.
func TestTextAreaObjectIsOneRune(t *testing.T) {
	app := &objectApp{text: objectText}
	tt := coreNewTester(app.build, 320, 120)
	ed := objectEditor(tt)
	tt.Press(100, 20)
	tt.Release(100, 20)
	tt.Key(0, KeyEnd)
	tt.Key(0, KeyLeft)
	tt.Key(0, KeyLeft)
	if ed.caret != 3 {
		t.Fatalf("caret at %d after two Left from the end, want 3 (after the object)", ed.caret)
	}
	tt.Key(0, KeyBackspace)
	if app.text != "abcd" {
		t.Errorf("Backspace after the object left %q, want %q", app.text, "abcd")
	}

	app.text = objectText
	tt.Frame()
	tt.Key(0, KeyHome)
	tt.Key(0, KeyRight)
	tt.Key(0, KeyRight)
	tt.Key(0, KeyDelete)
	if app.text != "abcd" {
		t.Errorf("Delete before the object left %q, want %q", app.text, "abcd")
	}
}

// Copying a selection that holds an object puts its ObjectText on the
// clipboard.
func TestTextAreaObjectCopiesAsText(t *testing.T) {
	app := &objectApp{text: objectText}
	tt := coreNewTester(app.build, 320, 120)
	tt.Press(100, 20)
	tt.Release(100, 20)
	tt.Key(Cmd, KeyA)
	tt.Key(Cmd, KeyC)
	if got, want := tt.Clipboard(), "ab[file]cd"; got != want {
		t.Errorf("copied %q, want %q", got, want)
	}
}

// A press on an object tells the app, with the object's box in the
// window, and puts the caret by it.
func TestTextAreaObjectPress(t *testing.T) {
	app := &objectApp{text: objectText}
	tt := coreNewTester(app.build, 320, 120)
	o := app.painted[len(app.painted)-1]
	x, y := o.Rect.X+o.Rect.W/2, o.Rect.Y+o.Rect.H/2
	tt.Press(x, y)
	tt.Release(x, y)
	if len(app.pressed) != 1 {
		t.Fatalf("%d presses reported, want 1", len(app.pressed))
	}
	if got := app.pressed[0]; got.Index != 2 || got.Rect.W != 40 || got.Rect.X != o.Rect.X || got.Rect.Y != o.Rect.Y {
		t.Errorf("press reported %+v, painted at %+v", got, o)
	}
	// A press on the text beside it is not one on the object.
	app.pressed = nil
	tt.Press(o.Rect.X-5, y)
	tt.Release(o.Rect.X-5, y)
	if len(app.pressed) != 0 {
		t.Errorf("a press beside the object reported %+v", app.pressed)
	}
}

// Ranges that make objects are not merged with each other or the ranges
// around them, and a range longer than a rune makes none.
func TestTextRangesObjects(t *testing.T) {
	red := RGB(220, 0, 0)
	ed := &editor{}
	ed.ranges = []TextRange{
		{Start: 0, End: 10, Color: red},
		{Start: 3, End: 4, ObjectWidth: 10, ObjectHeight: 8},
		{Start: 4, End: 5, ObjectWidth: 10, ObjectHeight: 8},
		{Start: 12, End: 13, ObjectWidth: 10, ObjectHeight: 8},
	}
	ed.sortRanges()
	want := []TextRange{
		{Start: 0, End: 3, Color: red},
		{Start: 3, End: 4, ObjectWidth: 10, ObjectHeight: 8},
		{Start: 4, End: 5, ObjectWidth: 10, ObjectHeight: 8},
		{Start: 5, End: 10, Color: red},
		{Start: 12, End: 13, ObjectWidth: 10, ObjectHeight: 8},
	}
	if len(ed.ranges) != len(want) {
		t.Fatalf("merged to %v, want %v", ed.ranges, want)
	}
	for i := range want {
		if ed.ranges[i] != want[i] {
			t.Errorf("run %d is %v, want %v", i, ed.ranges[i], want[i])
		}
	}
}
