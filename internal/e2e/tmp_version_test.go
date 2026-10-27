//go:build windows

package e2e

import (
	"os/exec"
	"testing"

	"github.com/egoist/mygo"
)

func TestTmpVersion(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "v", Width: 400, Height: 300})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitForPage(t, w.Page(), "document.readyState === 'complete'")
	script := `Get-CimInstance Win32_Process -Filter "Name='msedgewebview2.exe'" | Where-Object { $_.CommandLine -notmatch 'Packages' -and $_.CommandLine -notmatch 'type=' } | ForEach-Object { $_.ProcessId.ToString() + ' ' + $_.CommandLine }`
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	t.Log(string(out))
}
