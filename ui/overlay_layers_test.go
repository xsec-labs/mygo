package ui

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// layersView is a window with a page button, a dialog, and a drawer, which
// the view builds after the dialog: the order a page that asks "delete?"
// of an item in its drawer happens to build them in.
type layersView struct {
	dialog, drawer bool
	menuOpen       bool
	pageMenuOpen   bool
	dialogOK       int
	drawerAction   int
	picked         []string
	order          []string // the overlays built, in the order the view builds them
}

func (l *layersView) buildDrawer(c *context) {
	coreOverlayAt(c, LayerDrawer, func() {
		back := coreRow(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Justify(End).Background(RGBA(0, 0, 0, 0.2)).Modal()
		back.Children(func() {
			panel := coreColumn(c).Width(200).FillHeight().Background(RGB(250, 250, 250))
			panel.Children(func() {
				coreButton(c, "Drawer close")
				if coreButton(c, "Drawer action").Clicked() {
					l.drawerAction++
				}
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, KeyEscape) {
				l.drawer = false
			}
		})
	})
}

func (l *layersView) buildDialog(c *context) {
	coreModal(c, &l.dialog, func() {
		if coreButton(c, "Dialog OK").Clicked() {
			l.dialogOK++
			l.dialog = false
		}
		menu := coreButton(c, "Dialog menu")
		if menu.Clicked() {
			l.menuOpen = !l.menuOpen
		}
		corePopover(c, menu, &l.menuOpen, func() {
			if coreButton(c, "Dialog menu item").Clicked() {
				l.picked = append(l.picked, "dialog item")
			}
		})
	})
}

func (l *layersView) view(c *context) {
	page := coreButton(c, "Page button")
	if page.Clicked() {
		l.pageMenuOpen = !l.pageMenuOpen
	}
	corePopover(c, page, &l.pageMenuOpen, func() {
		if coreButton(c, "Page menu item").Clicked() {
			l.picked = append(l.picked, "page item")
		}
	})
	// The dialog first, the drawer after it.
	l.buildDialog(c)
	if l.drawer {
		l.buildDrawer(c)
	}
}

func TestDialogBuiltBeforeADrawerShowsAboveIt(t *testing.T) {
	l := &layersView{dialog: true, drawer: true}
	tt := coreNewTester(l.view, 600, 400)

	// The dialog has the keyboard, though the drawer was built later.
	if !tt.Focused("Dialog OK") {
		t.Fatal("the dialog does not have the focus the drawer built later should not take")
	}
	// The pointer reaches the dialog: the drawer's backdrop is below it.
	if err := tt.Click("Dialog OK"); err != nil {
		t.Fatal(err)
	}
	if l.dialogOK != 1 || l.dialog {
		t.Fatalf("a click on the dialog's button reached %d times, the dialog open %v: the drawer's backdrop took it", l.dialogOK, l.dialog)
	}
	// The dialog gone, the drawer is what is on top.
	if !l.drawer {
		t.Fatal("the drawer closed with the dialog")
	}
	if err := tt.Click("Drawer action"); err != nil {
		t.Fatal(err)
	}
	if l.drawerAction != 1 {
		t.Errorf("the drawer's button was clicked %d times, not once", l.drawerAction)
	}
}

func TestEscapeClosesTheDialogBeforeTheDrawerBelowIt(t *testing.T) {
	l := &layersView{dialog: true, drawer: true}
	tt := coreNewTester(l.view, 600, 400)
	tt.Key(0, KeyEscape)
	if l.dialog || !l.drawer {
		t.Fatalf("Escape: the dialog open %v, the drawer open %v; the dialog is on top and closes first", l.dialog, l.drawer)
	}
	tt.Key(0, KeyEscape)
	if l.drawer {
		t.Error("a second Escape did not close the drawer")
	}
}

func TestFocusReturnsAcrossLayers(t *testing.T) {
	l := &layersView{}
	tt := coreNewTester(l.view, 600, 400)
	// Opened one after the other, the dialog built before the drawer; closed,
	// the focus goes back to what had it in the drawer, not to the drawer's
	// first element, which the drawer's own focus trap would take it to.
	l.drawer = true
	tt.Frame()
	if err := tt.Click("Drawer action"); err != nil {
		t.Fatal(err)
	}
	l.dialog = true
	tt.Frame()
	if !tt.Focused("Dialog OK") {
		t.Fatal("the dialog did not take the focus")
	}
	tt.Key(0, KeyEscape)
	if l.dialog {
		t.Fatal("Escape did not close the dialog")
	}
	if !tt.Focused("Drawer action") {
		t.Error("the focus did not go back to the drawer's button as the dialog closed")
	}
}

func TestAssistiveTechnologySeesTheDialogAndAboveOnly(t *testing.T) {
	l := &layersView{dialog: true, drawer: true}
	tt := coreNewTester(l.view, 600, 400)
	tt.rt.access = true
	tt.Frame()
	var labels []string
	for _, n := range tt.rt.accessTree().Nodes {
		labels = append(labels, n.Label)
	}
	if !slices.Contains(labels, "Dialog OK") {
		t.Errorf("the dialog on top is not in the tree: %q", labels)
	}
	for _, hidden := range []string{"Drawer action", "Page button"} {
		if slices.Contains(labels, hidden) {
			t.Errorf("%q, behind the dialog, is in the tree: %q", hidden, labels)
		}
	}
	// With the dialog closed, the drawer is on top.
	l.dialog = false
	tt.Frame()
	labels = labels[:0]
	for _, n := range tt.rt.accessTree().Nodes {
		labels = append(labels, n.Label)
	}
	if !slices.Contains(labels, "Drawer action") || slices.Contains(labels, "Page button") {
		t.Errorf("with the dialog closed, the tree has %q", labels)
	}
	_ = platform.RoleButton
}

// A menu opened inside the dialog is part of the dialog's layer, after it:
// above a drawer built later, and not inert.
func TestMenuInADialogShowsAboveALaterDrawer(t *testing.T) {
	l := &layersView{dialog: true, drawer: true}
	tt := coreNewTester(l.view, 600, 400)
	if err := tt.Click("Dialog menu"); err != nil {
		t.Fatal(err)
	}
	if !l.menuOpen || !tt.HasText("Dialog menu item") {
		t.Fatal("the menu in the dialog did not open")
	}
	if err := tt.Click("Dialog menu item"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.picked, []string{"dialog item"}) {
		t.Errorf("the menu item was not reached through the drawer's backdrop: %q", l.picked)
	}
	// Escape closes the menu first, the overlay on top, not the dialog.
	l.menuOpen = true
	tt.Frame()
	tt.Key(0, KeyEscape)
	if l.menuOpen || !l.dialog {
		t.Errorf("Escape: the menu open %v, the dialog open %v", l.menuOpen, l.dialog)
	}
}

// A popover of the window's content is in the default layer: below the
// drawer's backdrop once a drawer shows.
func TestPopoverOfThePageIsBelowADrawer(t *testing.T) {
	l := &layersView{}
	tt := coreNewTester(l.view, 600, 400)
	if err := tt.Click("Page button"); err != nil {
		t.Fatal(err)
	}
	if !l.pageMenuOpen || !tt.HasText("Page menu item") {
		t.Fatal("the page's menu did not open")
	}
	l.drawer = true
	tt.Frame()
	r, ok := tt.Find("Page menu item")
	if !ok {
		t.Fatal("the page's menu item is gone")
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if len(l.picked) != 0 {
		t.Errorf("a click through the drawer's backdrop reached the page's menu: %q", l.picked)
	}
}

// Within a layer the order is the order of building, as it was for every
// overlay.
func TestOverlaysOfALayerKeepTheirBuildOrder(t *testing.T) {
	var closed []string
	open := map[string]bool{"first": true, "second": true}
	build := func(c *context, name string) {
		coreOverlay(c, func() {
			back := coreBox(c).Absolute().Left(0).Top(0).Right(0).Bottom(0)
			back.Children(func() { coreText(c, name) })
			if back.OverlayShortcut(0, KeyEscape) {
				open[name] = false
				closed = append(closed, name)
			}
		})
	}
	tt := coreNewTester(func(c *context) {
		for _, name := range []string{"first", "second"} {
			if open[name] {
				build(c, name)
			}
		}
	}, 300, 200)
	tt.Key(0, KeyEscape)
	tt.Key(0, KeyEscape)
	if !slices.Equal(closed, []string{"second", "first"}) {
		t.Errorf("Escape closed %q: the overlay built last is on top", closed)
	}
}

// Toasts show above a dialog and do not take the keyboard or the pointer
// from it.
func TestToastsShowAboveDialogs(t *testing.T) {
	l := &layersView{}
	tt := coreNewTester(func(c *context) {
		l.view(c)
		coreToastViewportBase(c, func(viewport *node, toasts []Toast) {})
	}, 600, 400)
	l.dialog = true
	tt.Frame()
	if !tt.Focused("Dialog OK") {
		t.Fatal("a toast viewport built after the dialog took its focus")
	}
	if err := tt.Click("Dialog OK"); err != nil {
		t.Fatal(err)
	}
	if l.dialogOK != 1 {
		t.Error("a toast viewport built after the dialog blocked the pointer")
	}
}

// A popover of the window's content closes at a press in any overlay above
// it, a drawer without a backdrop included: only overlays above LayerDefault
// stay open at a press in a higher layer.
func TestPopoverOfThePageClosesAtAPressInADrawer(t *testing.T) {
	pageOpen, drawer, actions := false, false, 0
	tt := coreNewTester(func(c *context) {
		page := coreButton(c, "Page button")
		if page.Clicked() {
			pageOpen = !pageOpen
		}
		corePopover(c, page, &pageOpen, func() { coreButton(c, "Page menu item") })
		if drawer {
			coreOverlayAt(c, LayerDrawer, func() {
				panel := coreColumn(c).Absolute().Right(0).Top(0).Bottom(0).Width(200).Background(RGB(250, 250, 250))
				panel.Children(func() {
					if coreButton(c, "Drawer action").Clicked() {
						actions++
					}
				})
			})
		}
	}, 600, 400)
	tt.Click("Page button")
	if !pageOpen {
		t.Fatal("the page's menu did not open")
	}
	drawer = true
	tt.Frame()
	if err := tt.Click("Drawer action"); err != nil {
		t.Fatal(err)
	}
	if actions != 1 || pageOpen {
		t.Errorf("a press in the drawer: its button clicked %d times, the page's menu open %v", actions, pageOpen)
	}
}

// A drawer stays open at a press in a dialog above it, and a popover opened
// in the drawer too.
func TestDrawerAndItsPopoverStayOpenAtAPressInADialog(t *testing.T) {
	l := &layersView{dialog: true, drawer: true}
	tt := coreNewTester(l.view, 600, 400)
	tt.Click("Dialog OK")
	if !l.drawer {
		t.Error("a press in the dialog closed the drawer below it")
	}
}

// A row a list builds as it lays out, inside a dialog, opens a popover in
// the dialog's layer: above a drawer built after the dialog, and reachable.
func TestPopoverInALazyRowOfADialogShowsAboveALaterDrawer(t *testing.T) {
	dialog, drawer, menu := true, true, false
	var picked []string
	state := ListState{}
	tt := coreNewTester(func(c *context) {
		coreModal(c, &dialog, func() {
			coreList(c, &state, 20, func(i int) {
				if i != 0 {
					coreText(c, "row").Height(20)
					return
				}
				more := coreButton(c, "Row menu")
				if more.Clicked() {
					menu = !menu
				}
				corePopover(c, more, &menu, func() {
					if coreButton(c, "Row menu item").Clicked() {
						picked = append(picked, "row item")
					}
				})
			}).Height(200)
		})
		if drawer {
			coreOverlayAt(c, LayerDrawer, func() {
				back := coreRow(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Justify(End).Background(RGBA(0, 0, 0, 0.2)).Modal()
				back.Children(func() { coreColumn(c).Width(200).FillHeight().Children(func() { coreButton(c, "Drawer action") }) })
			})
		}
	}, 600, 400)
	if err := tt.Click("Row menu"); err != nil {
		t.Fatal(err)
	}
	if !menu || !tt.HasText("Row menu item") {
		t.Fatal("the menu in the row did not open")
	}
	if err := tt.Click("Row menu item"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(picked, []string{"row item"}) {
		t.Errorf("the item of the menu opened in a row of the dialog's list was not reached: %q", picked)
	}
}

// A dialog whose button takes the focus as it is made (AutoFocus) gives
// the focus back to what had it before, not to that button, which is gone
// with the dialog.
func TestFocusGoesBackFromADialogWithAnAutoFocusedButton(t *testing.T) {
	open := false
	tt := coreNewTester(func(c *context) {
		if coreButton(c, "Open").Clicked() {
			open = true
		}
		coreModal(c, &open, func() {
			if coreButton(c, "Close").AutoFocus().Clicked() {
				open = false
			}
		})
	}, 400, 300)
	tt.Click("Open")
	if !tt.Focused("Close") {
		t.Fatal("the dialog's AutoFocus button did not take the focus")
	}
	tt.Key(0, KeyEscape)
	if open || !tt.Focused("Open") {
		t.Errorf("closed (open %v): the focus is not back on Open", open)
	}
}
