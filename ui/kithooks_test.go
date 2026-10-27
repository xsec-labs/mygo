package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestTooltipDelay(t *testing.T) {
	tt, now := clockTester(func(c *context) {
		coreColumn(c).Padding(40).Gap(20).Children(func() {
			coreButton(c, "Fast").Label("fast").TooltipDelay(300 * time.Millisecond).Tooltip("Fast tip")
			coreButton(c, "Now").Label("now").TooltipDelay(0).Tooltip("Now tip")
			coreButton(c, "Slow").Label("slow").Tooltip("Slow tip")
		})
	}, 300, 300)
	tick := ticker(tt, now)
	over := func(label string) {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
	}
	away := func() {
		tt.Move(290, 290)
		tick(time.Second) // longer than the tips stay warm
	}

	over("fast")
	tick(250 * time.Millisecond)
	if tt.HasText("Fast tip") {
		t.Error("the tip shows before its 300 ms")
	}
	tick(100 * time.Millisecond)
	if !tt.HasText("Fast tip") {
		t.Error("no tip after 350 ms with a delay of 300 ms")
	}
	away()

	over("now")
	tick(16 * time.Millisecond)
	if !tt.HasText("Now tip") {
		t.Error("a delay of zero does not show the tip at once")
	}
	away()

	over("slow")
	tick(350 * time.Millisecond)
	if tt.HasText("Slow tip") {
		t.Error("the default delay is shorter than 0.6 seconds")
	}
	tick(300 * time.Millisecond)
	if !tt.HasText("Slow tip") {
		t.Error("no tip after 0.65 seconds with the default delay")
	}
}

func TestDisabledFocusable(t *testing.T) {
	clicks := map[string]int{}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(20).Gap(10).Children(func() {
			for _, k := range []string{"first", "kept", "off", "last"} {
				b := coreButton(c, k).Label(k)
				switch k {
				case "kept":
					b.DisabledFocusable(true)
				case "off":
					b.Disabled(true)
				}
				if b.Clicked() {
					clicks[k]++
				}
			}
		})
	}, 300, 300)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})

	// Tab stops at it and not at a disabled button.
	tt.Key(0, KeyTab)
	if !tt.Focused("first") {
		t.Fatal("Tab does not reach the first button")
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("kept") {
		t.Fatal("Tab skips a DisabledFocusable button")
	}
	// It takes neither Enter nor Space.
	tt.Key(0, KeyEnter)
	tt.Key(0, KeySpace)
	if clicks["kept"] != 0 {
		t.Errorf("a DisabledFocusable button was clicked by the keys %d times", clicks["kept"])
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("last") {
		t.Error("Tab stops at a disabled button")
	}

	// A click gives it the focus, and is not a click.
	r, _ := tt.Find("kept")
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if !tt.Focused("kept") || clicks["kept"] != 0 {
		t.Errorf("pressing it: focused %v, %d clicks", tt.Focused("kept"), clicks["kept"])
	}
	// A disabled button keeps the focus where it was.
	r, _ = tt.Find("off")
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if tt.Focused("off") || clicks["off"] != 0 {
		t.Errorf("a disabled button: focused %v, %d clicks", tt.Focused("off"), clicks["off"])
	}

	// Assistive technology reads it as disabled.
	if n := accessNode(t, tt.h.access, platform.RoleButton, "kept"); n.States&platform.AccessDisabled == 0 {
		t.Error("a DisabledFocusable button is not disabled for assistive technology")
	}
}

func TestDisabledFocusableReplacedByDisabled(t *testing.T) {
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(20).Children(func() {
			coreButton(c, "b").Label("b").DisabledFocusable(true).Disabled(true)
		})
	}, 200, 100)
	tt.Key(0, KeyTab)
	if tt.Focused("b") {
		t.Error("Disabled after DisabledFocusable still takes the focus")
	}
}

func TestReleaseFocus(t *testing.T) {
	var field *node
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(20).Gap(10).Children(func() {
			field = coreButton(c, "A").Label("a").Focusable()
			coreButton(c, "B").Label("b")
			if field.Shortcut(0, KeyEscape) {
				field.ReleaseFocus()
			}
		})
	}, 200, 200)
	tt.Key(0, KeyTab)
	if !tt.Focused("a") {
		t.Fatal("no focus on a")
	}
	tt.Key(0, KeyEscape)
	if tt.Focused("a") || tt.rt.focused != 0 {
		t.Errorf("the focus stays after ReleaseFocus (focused id %d)", tt.rt.focused)
	}
	// Tab starts over from the first.
	tt.Key(0, KeyTab)
	if !tt.Focused("a") {
		t.Error("Tab after ReleaseFocus does not start from the first element")
	}
}

func TestDragThreshold(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slop  float32
		moves float32
		drags bool
	}{
		{"default, 5 DIPs", 0, 5, true},
		{"default, 3 DIPs", 0, 3, false},
		{"8, 6 DIPs", 8, 6, false},
		{"8, 9 DIPs", 8, 9, true},
		{"1, 2 DIPs", 1, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var src *node
			dragging := false
			tt := coreNewTester(func(c *context) {
				coreColumn(c).Padding(20).Children(func() {
					src = coreBox(c).Size(100, 40).Label("Source").Drag("v")
					if tc.slop > 0 {
						src.DragThreshold(tc.slop)
					}
					dragging = src.Dragging()
				})
			}, 300, 200)
			r, _ := tt.Find("Source")
			tt.Press(r.X+10, r.Y+10)
			tt.Move(r.X+10+tc.moves, r.Y+10)
			if dragging != tc.drags {
				t.Errorf("after %v DIPs with a threshold of %v: dragging %v, want %v", tc.moves, tc.slop, dragging, tc.drags)
			}
			tt.Release(r.X+10+tc.moves, r.Y+10)
		})
	}
}
