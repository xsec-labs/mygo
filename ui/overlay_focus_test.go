package ui

import "testing"

// TestEscapeGivesTheFocusBackVisibly checks that a dialog closed with Escape
// gives the focus back to the button that opened it, and that the focus shows
// as Tab's does: the click that opened the dialog left it without a ring.
func TestEscapeGivesTheFocusBackVisibly(t *testing.T) {
	open := false
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(20).Gap(10).Children(func() {
			if coreButton(c, "Open").Label("open").Clicked() {
				open = true
			}
			coreDialogBase(c, &open, func(backdrop, panel *node) {
				coreButton(c, "Inside").Label("inside")
			})
		})
	}, 300, 200)
	if err := tt.Click("open"); err != nil {
		t.Fatal(err)
	}
	if !open || !tt.Focused("inside") {
		t.Fatalf("the dialog does not open with the focus in it (open %v)", open)
	}
	tt.Key(0, KeyEscape)
	if open {
		t.Fatal("Escape does not close the dialog")
	}
	if !tt.Focused("open") {
		t.Errorf("the focus does not go back to the opener (focused id %d)", tt.rt.focused)
	}
	if !tt.rt.focusVisible {
		t.Error("the focus given back by Escape has no ring")
	}
}
