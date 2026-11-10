package ui

import "testing"

// A chosen row or cell fills with the theme's Chosen color and its text
// with ChosenText when the theme sets them, so a subtle tint leaves the text
// readable; a theme that leaves them unset keeps the accent, with AccentText
// on it.
func TestChosenRowsUseChosenColors(t *testing.T) {
	tint, tintText := RGBA(37, 99, 235, 0.12), Hex("#18181b")
	for _, cells := range []bool{false, true} {
		for _, tinted := range []bool{false, true} {
			th := LightTheme()
			wantFill, wantText := th.Accent, th.AccentText
			if tinted {
				th.Chosen, th.ChosenText = tint, tintText
				wantFill, wantText = tint, tintText
			}
			sel := 0
			s := &ListState{Selected: &sel}
			if cells {
				s.CellSelect = true
				s.Selection = &Selection[int]{}
			}
			var text *node
			tt := NewTester(func(c *Context) {
				c.SetTheme(th)
				Table(c, s, []TableColumn{{Title: "A", Width: 120}}, 3, func(row, col int) {
					n := Textf(c, "r%d", row).node()
					if row == 1 {
						text = n
					}
				}).Grow(1)
			}, 400, 300)
			tt.Frame()
			tt.Click("r1")
			var chosen *node
			for p := text.parent; p != nil && chosen == nil; p = p.parent {
				if p.checked == 2 {
					chosen = p
				}
			}
			if chosen == nil {
				t.Fatalf("cells=%v tinted=%v: the row does not show as chosen", cells, tinted)
			}
			if chosen.bg != wantFill || chosen.ts.color != wantText {
				t.Fatalf("cells=%v tinted=%v: chosen fill %v text %v; want fill %v text %v", cells, tinted, chosen.bg, chosen.ts.color, wantFill, wantText)
			}
		}
	}
}

// chosen falls back to the accent when Chosen is unset, and when it is set
// takes ChosenText and the cursor border from the theme, each falling back
// to Text when the theme leaves it transparent.
func TestChosenFallbacks(t *testing.T) {
	th := LightTheme()
	fill, text, cursor := th.chosen()
	if fill != th.Accent || text != th.AccentText || cursor != th.AccentText {
		t.Fatalf("unset: fill %v text %v cursor %v; want the accent %v, %v, %v", fill, text, cursor, th.Accent, th.AccentText, th.AccentText)
	}
	tint, focus := RGBA(37, 99, 235, 0.12), RGBA(255, 0, 0, 1)
	th.Chosen, th.ChosenText, th.Focus = tint, Transparent, focus
	fill, text, cursor = th.chosen()
	if fill != tint || text != th.Text || cursor != focus {
		t.Fatalf("set, text and cursor unset: fill %v text %v cursor %v; want %v, %v, %v", fill, text, cursor, tint, th.Text, focus)
	}
	th.ChosenText, th.Focus = Hex("#18181b"), Transparent
	fill, text, cursor = th.chosen()
	if text != th.ChosenText || cursor != th.Text {
		t.Fatalf("set, focus unset: text %v cursor %v; want %v and %v", text, cursor, th.ChosenText, th.Text)
	}
}
