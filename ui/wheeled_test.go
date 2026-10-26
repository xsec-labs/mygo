package ui

import (
	"testing"
	"time"
)

// An element asking for Ctrl+wheel reads the notches turned with exactly
// Ctrl held over it; the scroll container around it still scrolls with the
// wheel on its own, and Ctrl+wheel does not scroll it.
func TestWheeledWithModifiers(t *testing.T) {
	var page ScrollState
	var dx, dy float32
	var ok bool
	tt := NewTester(func(c *Context) {
		Scroll(c).Fill().TrackScroll(&page).Children(func() {
			zoom := Box(c).Size(200, 100).Shrink(0)
			if x, y, got := zoom.Wheeled(Ctrl); got {
				dx, dy, ok = x, y, got
			}
			Box(c).Height(1000).Shrink(0)
		})
	}, 200, 200)
	defer tt.rt.close()
	tt.Frame()

	tt.Move(50, 50)
	tt.WheelWith(Ctrl, 50, 50, 0, -40)
	tt.Advance(time.Second)
	if !ok || dx != 0 || dy != -40 {
		t.Errorf("Ctrl+wheel read dx %v, dy %v, ok %v; want 0, -40, true", dx, dy, ok)
	}
	if page.Y != 0 {
		t.Errorf("Ctrl+wheel scrolled the container to %v", page.Y)
	}

	ok = false
	tt.Wheel(50, 50, 0, 40)
	tt.Advance(time.Second)
	if ok {
		t.Error("the wheel on its own was read as Ctrl+wheel")
	}
	if page.Y != 40 {
		t.Errorf("the wheel on its own scrolled the container to %v, want 40", page.Y)
	}

	// Only exactly the modifiers asked for.
	tt.WheelWith(Ctrl|Shift, 50, 50, 0, 40)
	tt.Advance(time.Second)
	if ok {
		t.Error("Ctrl+Shift+wheel was read as Ctrl+wheel")
	}

	// Outside the element, the wheel with Ctrl is nobody's.
	tt.WheelWith(Ctrl, 50, 150, 0, -40)
	if ok {
		t.Error("Ctrl+wheel outside the element was read by it")
	}
}

// Notches that come before the view draws again add up, and the element
// reads them once.
func TestWheeledAddsUpAndForgets(t *testing.T) {
	var sum float32
	var reads int
	tt := NewTester(func(c *Context) {
		e := Box(c).Fill()
		if _, y, ok := e.Wheeled(0); ok {
			sum += y
			reads++
		}
	}, 100, 100)
	defer tt.rt.close()
	tt.Frame()

	tt.Move(50, 50)
	tt.rt.scroll(0, 10, 0, false, false)
	tt.rt.scroll(0, 15, 0, false, false)
	tt.Frame()
	if sum != 25 || reads != 1 {
		t.Errorf("two notches read as %v in %d frames, want 25 in 1", sum, reads)
	}
	tt.Frame()
	if sum != 25 || reads != 1 {
		t.Errorf("the notches were read again: %v in %d frames", sum, reads)
	}
}

// The innermost element asking takes the notch, a disabled one takes none,
// and an element asking for another combination takes none either.
func TestWheeledInnermostAndDisabled(t *testing.T) {
	var outer, inner, other float32
	var disabled bool
	tt := NewTester(func(c *Context) {
		out := Box(c).Fill()
		if _, y, ok := out.Wheeled(Alt); ok {
			outer += y
		}
		out.Children(func() {
			in := Box(c).Size(100, 100).Shrink(0)
			if disabled {
				in.Disabled(true)
			}
			if _, y, ok := in.Wheeled(Alt); ok {
				inner += y
			}
			if _, y, ok := in.Wheeled(Super); ok {
				other += y
			}
		})
	}, 200, 200)
	defer tt.rt.close()
	tt.Frame()

	tt.Move(50, 50)
	tt.WheelWith(Alt, 50, 50, 0, 10)
	if inner != 10 || outer != 0 || other != 0 {
		t.Errorf("inner %v, outer %v, other %v; want 10, 0, 0", inner, outer, other)
	}
	disabled = true
	tt.Frame()
	tt.WheelWith(Alt, 50, 50, 0, 10)
	if inner != 10 || outer != 10 {
		t.Errorf("with the inner disabled, inner %v, outer %v; want 10, 10", inner, outer)
	}
}

// The wheel is read in the element's own axes: a box drawn twice as wide
// turns the notch half as far along x.
func TestWheeledInLocalAxes(t *testing.T) {
	var dx, dy float32
	tt := NewTester(func(c *Context) {
		e := Box(c).Size(100, 100).Shrink(0).Scale(2, 1)
		if x, y, ok := e.Wheeled(0); ok {
			dx, dy = x, y
		}
	}, 200, 200)
	defer tt.rt.close()
	tt.Frame()

	tt.Move(50, 50)
	tt.Wheel(50, 50, 10, 10)
	if dx != 5 || dy != 10 {
		t.Errorf("wheel (10, 10) over a box scaled (2, 1) read as (%v, %v), want (5, 10)", dx, dy)
	}
}
