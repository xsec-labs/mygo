package ui

import (
	"math"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestField(t *testing.T) {
	name, email, font, size := "", "", "Arial", "Small"
	notify, express := false, false
	emailError := "Enter an email address."
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Gap(12).Children(func() {
			coreField(c, "Name", func() { coreTextInput(c, &name) }).Description("As on your passport.")
			coreField(c, "Email", func() { coreTextInput(c, &email) }).Description("For receipts.").Error(emailError)
			coreField(c, "Notifications", func() { coreCheckbox(c, &notify, "Send emails") })
			coreField(c, "Delivery", func() {
				coreRadioGroup(c, func() {
					coreRadio(c, &size, "Small", "Small")
					coreRadio(c, &size, "Large", "Large")
				})
			})
			coreField(c, "Font", func() { coreCombobox(c, &font, fonts) })
			coreField(c, "Express", func() { coreSwitch(c, &express) })
			coreButton(c, "Cut").Tooltip("Cut the selection")
		})
	}, 500, 700)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	// The label names the control, and the texts below describe it, the
	// error first.
	if n := accessNode(t, tree, platform.RoleTextField, "Name"); n.Description != "As on your passport." || n.States&platform.AccessInvalid != 0 {
		t.Errorf("Name: %+v", n)
	}
	if n := accessNode(t, tree, platform.RoleTextField, "Email"); n.Description != emailError+"\nFor receipts." || n.States&platform.AccessInvalid == 0 {
		t.Errorf("Email: %+v", n)
	}
	if !tt.HasText(emailError) || !tt.HasText("For receipts.") {
		t.Errorf("the texts below do not show: %q", tt.Texts())
	}
	// A check box names itself; the field names the group around it.
	accessNode(t, tree, platform.RoleCheckBox, "Send emails")
	accessNode(t, tree, platform.RoleGroup, "Notifications")
	accessNode(t, tree, platform.RoleRadioGroup, "Delivery")
	accessNode(t, tree, platform.RoleComboBox, "Font")
	accessNode(t, tree, platform.RoleSwitch, "Express")
	if n := accessNode(t, tree, platform.RoleButton, "Cut"); n.Description != "Cut the selection" {
		t.Errorf("the tooltip does not describe the button: %+v", n)
	}
	// A click on the label focuses the control, or clicks it.
	tt.Click("Name")
	if !tt.Focused("Name") {
		t.Error("a click on the label did not focus the input")
	}
	tt.Type("Ada")
	if name != "Ada" {
		t.Errorf("typed %q", name)
	}
	// The text, not the group it names.
	var label Rect
	for _, n := range tt.rt.labels {
		if n.text == "Notifications" {
			label = n.r
		}
	}
	tt.ClickAt(label.X+label.W/2, label.Y+label.H/2)
	if !notify {
		t.Error("a click on the label did not check the check box")
	}
	tt.Click("Express")
	if !express {
		t.Error("a click on the label did not turn the switch on")
	}
	// In a group, the one chosen.
	tt.Click("Delivery")
	if !tt.Focused("Small") || size != "Small" {
		t.Errorf("a click on the label of a radio group: focused %v, chose %q", tt.Focused("Small"), size)
	}
}

func TestFieldWith(t *testing.T) {
	token, name := "", ""
	helped := 0
	form := false
	view := func(c *context) {
		fields := func() {
			coreFieldWith(c, "Token", func() {
				if coreButtonBase(c).Label("About the token").Size(16, 16).Clicked() {
					helped++
				}
			}, func() { coreTextInput(c, &token) })
			coreField(c, "Name", func() { coreTextInput(c, &name) })
		}
		if form {
			coreForm(c, fields)
		} else {
			coreColumn(c).Gap(12).Children(fields)
		}
	}
	tt := coreNewTester(view, 500, 300)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	// The label still names the control, not what is beside it.
	accessNode(t, tt.h.access, platform.RoleTextField, "Token")
	accessNode(t, tt.h.access, platform.RoleButton, "About the token")
	// What is beside the label follows its text, on its line, above the
	// control.
	label, _ := tt.Find("Token")
	about, _ := tt.Find("About the token")
	plain, _ := tt.Find("Name")
	if about.X < label.X+label.W || about.X > label.X+label.W+16 {
		t.Errorf("beside does not follow the label's text: label %+v, beside %+v", label, about)
	}
	if mid := about.Y + about.H/2; mid < label.Y || mid > label.Y+label.H {
		t.Errorf("beside is not on the label's line: label %+v, beside %+v", label, about)
	}
	if label.H != plain.H {
		t.Errorf("beside changed the height of the label: %v, a plain one %v", label.H, plain.H)
	}
	// A click on it is its own, and a click on the label still focuses the
	// control.
	tt.Click("About the token")
	if helped != 1 || tt.Focused("Token") {
		t.Errorf("a click beside the label: clicked %d, the input focused %v", helped, tt.Focused("Token"))
	}
	tt.ClickAt(label.X+label.W/2, label.Y+label.H/2)
	if !tt.Focused("Token") {
		t.Error("a click on the label did not focus the input")
	}
	tt.Type("abc")
	if token != "abc" {
		t.Errorf("typed %q", token)
	}
	// The Tab key reaches what is beside the label before the control, as
	// they read.
	tt.Click("Name")
	tt.Key(Shift, KeyTab)
	if !tt.Focused("Token") {
		t.Error("Shift+Tab from the next field did not reach the control")
	}
	tt.Key(Shift, KeyTab)
	if !tt.Focused("About the token") {
		t.Error("Shift+Tab from the control did not reach what is beside the label")
	}
	tt.Key(0, KeyTab)
	if !tt.Focused("Token") {
		t.Error("Tab from what is beside the label did not reach the control")
	}
	// In a form the labels sit in a column of their own, and what is
	// beside one stays with it, before the control.
	form = true
	tt.Frame()
	label, _ = tt.Find("Token")
	about, _ = tt.Find("About the token")
	var input Rect
	for _, n := range tt.h.access.Nodes {
		if n.Role == platform.RoleTextField && n.Label == "Token" {
			input = Rect{X: float32(n.Bounds.X), Y: float32(n.Bounds.Y), W: float32(n.Bounds.W), H: float32(n.Bounds.H)}
		}
	}
	if about.X < label.X+label.W || about.X+about.W > input.X {
		t.Errorf("in a form, beside is not between the label and the control: label %+v, beside %+v, input %+v", label, about, input)
	}
}

// What is beside the label is not the field's control, even when it is the
// only element of the field that takes the focus.
func TestFieldWithBesideIsNotTheControl(t *testing.T) {
	helped := 0
	var field *node
	tt := coreNewTester(func(c *context) {
		field = coreFieldWith(c, "Plan", func() {
			if coreButtonBase(c).Label("About the plan").Size(16, 16).Clicked() {
				helped++
			}
		}, func() { coreText(c, "Free") })
	}, 500, 300)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	if field.field.control != nil {
		t.Errorf("the field took %q for its control", field.field.control.label)
	}
	// The button keeps its own name, and a click on the label does nothing
	// to it.
	if n := accessNode(t, tt.h.access, platform.RoleButton, "About the plan"); n.Label != "About the plan" {
		t.Errorf("the label named what is beside it: %+v", n)
	}
	label, _ := tt.Find("Plan")
	tt.ClickAt(label.X+label.W/2, label.Y+label.H/2)
	if helped != 0 || tt.Focused("About the plan") {
		t.Errorf("a click on the label: clicked %d, focused %v", helped, tt.Focused("About the plan"))
	}
}

func TestFieldDisabled(t *testing.T) {
	name, saved := "", 0
	tt := coreNewTester(func(c *context) {
		coreFieldset(c, "Account", func() {
			coreField(c, "Name", func() { coreTextInput(c, &name) })
			if coreButton(c, "Save").Clicked() {
				saved++
			}
		}).Disabled(true)
	}, 400, 300)
	// The fieldset disables what is inside it, though it does so after
	// building it.
	tt.Click("Name")
	if tt.Focused("Name") {
		t.Error("a click on the label focused a disabled input")
	}
	tt.Click("Save")
	if saved != 0 || tt.Focused("Save") {
		t.Errorf("a disabled button: %d clicks, focused %v", saved, tt.Focused("Save"))
	}
	r, _ := tt.Find("Name")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H+20) // the input
	if tt.Focused("Name") {
		t.Error("a click focused a disabled input")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	if n := accessNode(t, tt.h.access, platform.RoleGroup, "Account"); n.States&platform.AccessDisabled == 0 {
		t.Errorf("the fieldset: %+v", n)
	}
}

func TestFormLayout(t *testing.T) {
	name, email, city := "", "", ""
	notify, express := false, false
	tt := coreNewTester(func(c *context) {
		coreForm(c, func() {
			coreField(c, "Name", func() { coreTextInput(c, &name) })
			coreField(c, "Email address", func() { coreTextInput(c, &email) }).Description("For receipts.")
			coreField(c, "Notifications", func() { coreCheckbox(c, &notify, "Send emails") })
			// A description under a control without text leaves its label
			// level with the control, not with the description.
			coreField(c, "Express", func() { coreSwitch(c, &express) }).Description("Ships the next day.")
			// Beside a dot drawn before it, the text of a row lines up
			// with the label.
			coreField(c, "Status", func() {
				coreRow(c).Gap(6).AlignItems(Start).Children(func() {
					coreBox(c).Width(8).Height(8).Background(RGB(0, 200, 0))
					coreText(c, "Online")
				})
			})
			coreFieldset(c, "Shipping", func() {
				coreField(c, "City", func() { coreTextInput(c, &city) })
			})
		}).Width(480)
	}, 500, 500)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	box := func(role platform.AccessRole, name string) platform.RectF {
		return accessNode(t, tree, role, name).Bounds
	}
	find := func(s string) Rect {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %q", s)
		}
		return r
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1 }
	// The controls start at one edge, beside the widest label, those of a
	// fieldset too, and the labels end at one.
	nameIn, emailIn, cityIn := box(platform.RoleTextField, "Name"), box(platform.RoleTextField, "Email address"), box(platform.RoleTextField, "City")
	if !near(nameIn.X, emailIn.X) || !near(nameIn.X, cityIn.X) || !near(nameIn.X, box(platform.RoleCheckBox, "Send emails").X) {
		t.Errorf("the controls start at %v, %v, %v", nameIn.X, emailIn.X, cityIn.X)
	}
	nameL, emailL := find("Name"), find("Email address")
	if !near(float64(nameL.X+nameL.W), float64(emailL.X+emailL.W)) || float64(emailL.X+emailL.W) > emailIn.X {
		t.Errorf("the labels end at %v and %v, the inputs start at %v", nameL.X+nameL.W, emailL.X+emailL.W, emailIn.X)
	}
	// Labels are level with the text of their control: inside the input's
	// border and padding, on the check box's text, and centered on a
	// switch.
	if !near(float64(nameL.Y), nameIn.Y+7) || !near(float64(emailL.Y), emailIn.Y+7) {
		t.Errorf("the labels are at %v and %v, the inputs at %v and %v", nameL.Y, emailL.Y, nameIn.Y, emailIn.Y)
	}
	if notifyL, own := find("Notifications"), find("Send emails"); !near(float64(notifyL.Y), float64(own.Y)) {
		t.Errorf("the label is at %v, the check box's text at %v", notifyL.Y, own.Y)
	}
	sw := box(platform.RoleSwitch, "Express")
	if l := find("Express"); math.Abs(float64(l.Y+l.H/2)-(sw.Y+sw.H/2)) > 1.5 {
		t.Errorf("the label's center is at %v, the switch's at %v", l.Y+l.H/2, sw.Y+sw.H/2)
	}
	if l, v := find("Status"), find("Online"); !near(float64(l.Y), float64(v.Y)) {
		t.Errorf("the label is at %v, the text beside the dot at %v", l.Y, v.Y)
	}
	// The description sits below its input, in the controls' column.
	if d := find("For receipts."); !near(float64(d.X), emailIn.X) || float64(d.Y) < emailIn.Y+emailIn.H {
		t.Errorf("the description is at %v, the input at %v", d, emailIn)
	}
}
