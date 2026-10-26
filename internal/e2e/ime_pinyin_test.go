//go:build !js

package e2e

import (
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// T-01 input method check: a Chinese pinyin composition, as an input method
// sends it, in the three controls the Xsec chat uses. The pinyin grows letter
// by letter as a preedit (with the caret at its end), then the input method
// commits the candidate. The test drives the window's native text input
// client in the process (no mouse or keyboard of the desktop), and reads the
// text the control holds.
const (
	pinyinReport = "shentouceshibaogao"
	hanziReport  = "渗透测试报告"
)

// pinyinPreedits are the steps the pinyin goes through as it is typed.
var pinyinPreedits = []string{"s", "shen", "shentou", "shentouce", "shentouceshi", "shentouceshibaogao"}

func composeReport(t *testing.T, w *mygo.Window, text func() string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("checked on macOS (NSTextInputClient) and Windows (IMM32) only")
	}
	for _, p := range pinyinPreedits {
		if !composeOver(w, p, len(p), false, -1, 0) {
			t.Fatalf("preedit %q: the native composition hook failed", p)
		}
	}
	// While the preedit is open, the control either keeps it out of its value
	// (TextInput, TextArea) or shows the whole of it in place (code editor);
	// never a part of it.
	eventually(t, "the whole preedit, or none of it, in the value", func() bool { v := text(); return v == "" || v == pinyinReport })
	t.Logf("value during the preedit: %q", text())
	if !composeOver(w, hanziReport, len([]rune(hanziReport)), true, -1, 0) {
		t.Fatalf("commit: the native composition hook failed")
	}
	eventually(t, "the committed text "+hanziReport, func() bool { return text() == hanziReport })
}

func TestT01PinyinTextInput(t *testing.T) {
	var frames atomic.Int32
	var line string
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().Padding(20).Children(func() { ui.TextInput(c, &line) })
	}
	text := func() (s string) {
		mygo.RunOnMain(func() { s = line })
		return s
	}
	w := newWindow(t, mygo.WindowOptions{Title: "T-01 TextInput", Width: 400, Height: 200, Content: ui.View(view)})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	if _, _, ok := inputClient(w); !ok {
		t.Skip("input method automation not available on this platform")
	}
	if !click(w, 200, 36) {
		t.Skip("click automation not available on this platform")
	}
	composeReport(t, w, text)
}

func TestT01PinyinTextArea(t *testing.T) {
	var frames atomic.Int32
	var notes string
	view := func(c *ui.Context) {
		frames.Add(1)
		ui.Column(c).Fill().Padding(20).Children(func() { ui.TextArea(c, &notes).Height(120) })
	}
	text := func() (s string) {
		mygo.RunOnMain(func() { s = notes })
		return s
	}
	w := newWindow(t, mygo.WindowOptions{Title: "T-01 TextArea", Width: 400, Height: 200, Content: ui.View(view)})
	eventually(t, "a frame", func() bool { return frames.Load() > 0 })
	if _, _, ok := inputClient(w); !ok {
		t.Skip("input method automation not available on this platform")
	}
	if !click(w, 200, 50) {
		t.Skip("click automation not available on this platform")
	}
	composeReport(t, w, text)
}

func TestT01PinyinCodeEditor(t *testing.T) {
	var frames atomic.Int32
	st := &editor.State{Buffer: editor.NewBuffer("")}
	w := newWindow(t, mygo.WindowOptions{Title: "T-01 Code editor", Width: 500, Height: 240, Content: ui.View(func(c *ui.Context) {
		editor.View(c, st).Fill().AutoFocus()
		frames.Add(1)
	})})
	text := func() (s string) {
		mygo.RunOnMain(func() { s = st.Buffer.String() })
		return s
	}
	eventually(t, "an editor frame", func() bool { return frames.Load() > 0 })
	if _, _, ok := inputClient(w); !ok {
		t.Skip("native input automation unavailable")
	}
	if !click(w, 300, 14) {
		t.Skip("click automation unavailable")
	}
	composeReport(t, w, text)
}
