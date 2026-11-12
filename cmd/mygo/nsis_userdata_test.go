package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestParseUserData(t *testing.T) {
	for _, tc := range []struct {
		entry, root, rel string
	}{
		{`$APPDATA\xSec Desktop`, "APPDATA", `xSec Desktop`},
		{`$LOCALAPPDATA/XSec`, "LOCALAPPDATA", `XSec`},
		{`$APPDATA\John's Data\logs`, "APPDATA", `John's Data\logs`},
	} {
		f, err := parseUserData(tc.entry)
		if err != nil {
			t.Errorf("parseUserData(%q): %v", tc.entry, err)
			continue
		}
		if f.root != tc.root || f.rel != tc.rel {
			t.Errorf("parseUserData(%q) = %+v, want root %s and rel %q", tc.entry, f, tc.root, tc.rel)
		}
	}
	// Each of these would delete more than the app's own folder, or
	// names NSIS or PowerShell cannot take.
	for _, entry := range []string{
		`$APPDATA`, `$APPDATA\`, `$APPDATA\..`, `$APPDATA\..\Documents`, `$APPDATA\a\\b`,
		`$APPDATA\.`, `$APPDATA\trailing.`, `$APPDATA\trailing `, ` $APPDATA\x`,
		`$APPDATA\x$y`, `$APPDATA\bad:name`, `$APPDATA\"quoted"`, "$APPDATA\\tab\tname",
		`C:\Users\me\Documents`, `%APPDATA%\xSec`, `$USERPROFILE\Documents`, `$appdata\x`,
	} {
		if _, err := parseUserData(entry); err == nil {
			t.Errorf("parseUserData(%q) accepted an entry it must refuse", entry)
		}
	}
}

func TestUserDataConfigIsChecked(t *testing.T) {
	dir := testModule(t, map[string]string{
		"main.go":   "package main\n\nfunc main() {}\n",
		"mygo.json": `{"name": "Userdata", "identifier": "com.example.userdata", "version": "1.0.0", "windows": {"userData": ["$APPDATA\\..\\Documents"]}}`,
	})
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "windows.userData") {
		t.Fatalf("loadConfig accepted a userData folder outside the app's own: %v", err)
	}
}

// userDataScript returns the parts of the uninstaller of an app with the
// given windows.userData.
func userDataScript(t *testing.T, userData []string) userDataParts {
	t.Helper()
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = userData
	parts, err := userDataUninstall(c)
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

func TestUserDataUninstallParts(t *testing.T) {
	if got := userDataScript(t, nil); got != (userDataParts{}) {
		t.Fatalf("without windows.userData the installer changes: %+v", got)
	}
	parts := userDataScript(t, []string{`$APPDATA\xSec Desktop`, `$LOCALAPPDATA/XSec`})
	for _, want := range []string{
		`!include "FileFunc.nsh"`, `!insertmacro un.GetOptions`, `Var DeleteUserData`,
	} {
		if !strings.Contains(parts.head, want) {
			t.Errorf("head lacks %q", want)
		}
	}
	if parts.page != "UninstPage custom un.UserDataPage un.UserDataLeave\n" {
		t.Errorf("page = %q", parts.page)
	}
	// Silent uninstalls keep the data, and /PURGE deletes it without asking.
	for _, want := range []string{
		`StrCpy $DeleteUserData 0`, `${un.GetOptions} $R0 "/PURGE" $R1`,
		`IfSilent 0 +2`, `${If} $DeleteUserData == 1`, `Call un.DeleteUserData`,
	} {
		if !strings.Contains(parts.functions+parts.call+parts.page, want) {
			t.Errorf("installer lacks %q", want)
		}
	}
	// The checkbox is not checked by default: only the page sets the flag.
	if strings.Contains(parts.functions, `StrCpy $UserDataCheckbox 1`) || strings.Contains(parts.functions, `${NSD_Check}`) {
		t.Error("the checkbox is checked by default")
	}
	// The page shows where the data is: NSIS expands the variables.
	if !strings.Contains(parts.functions, `$APPDATA\xSec Desktop`) || !strings.Contains(parts.functions, `$LOCALAPPDATA\XSec`) {
		t.Error("the page does not name the folders")
	}
	// The uninstaller runs the script the installer puts in the app's folder.
	if !strings.Contains(parts.functions, `-File "$INSTDIR\`+purgeScriptName+`"`) {
		t.Error("the uninstaller does not run the purge script")
	}
	// The purge removes each link in the folders without following it.
	for _, want := range []string{
		`Remove-Top $env:APPDATA 'xSec Desktop'`,
		`Remove-Top $env:LOCALAPPDATA 'XSec'`,
		`[IO.Directory]::Delete($p, $false)`,
		`ReparsePoint`,
		`exit 1`,
	} {
		if !strings.Contains(parts.script, want) {
			t.Errorf("purge script lacks %q", want)
		}
	}
	// The folders on the way to a listed one are checked for links first.
	if !strings.Contains(parts.script, "Split(") {
		t.Error("the purge script does not check the folders on the way to a listed one")
	}
	if strings.Contains(parts.script, "-Recurse") || strings.Contains(parts.script, "rmdir") || strings.Contains(parts.script, "RemoveDirectory") {
		t.Error("purge script must not use a recursive delete that could follow links")
	}
}

// NSIS cuts a string at nsisMaxString when the uninstaller runs: no line of
// its code may be that long, and a page too long for it is refused.
func TestUserDataFitsNSIS(t *testing.T) {
	parts := userDataScript(t, []string{`$APPDATA\xSec Desktop`, `$APPDATA\XSec`, `$LOCALAPPDATA\XSec`})
	for _, part := range []string{parts.head, parts.page, parts.call, parts.functions} {
		for _, line := range strings.Split(part, "\n") {
			if len(line) > nsisMaxString {
				t.Errorf("a line of the uninstaller is %d characters: NSIS cuts it at %d", len(line), nsisMaxString)
			}
		}
	}
	var long []string
	for i := 0; i < 6; i++ {
		long = append(long, `$APPDATA\`+strings.Repeat("N", 120)+strconv.Itoa(i))
	}
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = long
	if _, err := userDataUninstall(c); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("userDataUninstall accepted folders that the page cannot show: %v", err)
	}
}

// buildUserDataInstaller builds the installer of an app named Setup Test
// whose windows.userData is userData. It returns the setup program, and the
// folder the build wrote its files to.
func buildUserDataInstaller(t *testing.T, userData []string) (setup, work string) {
	t.Helper()
	list, err := json.Marshal(userData)
	if err != nil {
		t.Fatal(err)
	}
	dir := testModule(t, map[string]string{
		"main.go":              "package main\n\nfunc main() {}\n",
		"mygo.json":            `{"name": "Setup Test", "identifier": "com.example.setuptest", "version": "2.0.0", "windows": {"signCommand": "echo %1>>signed.txt", "userData": ` + string(list) + `}}`,
		"resources/data/a.txt": "a",
		"resources/icon.png":   string(defaultIcon()),
	})
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	work = t.TempDir()
	opts := buildOptions{sign: "-", work: work}
	if opts.pkg, err = packageDir(c); err != nil {
		t.Fatal(err)
	}
	artifacts, err := buildPlatform(c, "windows", "amd64", opts)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts[len(artifacts)-1], work
}

// installSilently installs setup into a new folder, silently, and returns it.
func installSilently(t *testing.T, setup string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "SetupTest")
	if out, err := exec.Command(setup, "/S", "/D="+dir).CombinedOutput(); err != nil {
		t.Fatalf("installing: %v\n%s", err, out)
	}
	return dir
}

// TestWindowsInstallerUserData builds an installer that offers to delete a
// folder of the user's data, then checks what the uninstaller does with it.
// It compiles wherever NSIS is installed; on Windows it installs the app and
// uninstalls it: kept by default, and deleted with /PURGE, where a junction
// in the data must go without taking its target, a folder the user works in.
func TestWindowsInstallerUserData(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	const folder = "Setup Test Userdata"
	setup, work := buildUserDataInstaller(t, []string{`$APPDATA\` + folder})
	script, err := os.ReadFile(filepath.Join(work, "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "UninstPage custom un.UserDataPage un.UserDataLeave") {
		t.Fatal("the uninstaller does not offer to delete the data")
	}
	// The purge script goes into the app's folder, next to the uninstaller.
	if !strings.Contains(string(script), `File "/oname=`+purgeScriptName+`"`) {
		t.Fatal("the installer does not install the purge script")
	}
	purge, err := os.ReadFile(filepath.Join(work, purgeScriptName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(purge, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("the purge script has no byte-order mark: PowerShell would misread non-ASCII folder names")
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Fatal("APPDATA is not set")
	}
	data := filepath.Join(appData, folder)
	if _, err := os.Stat(data); err == nil {
		t.Fatalf("%s exists: the test would delete it", data)
	}
	t.Cleanup(func() { os.RemoveAll(data) })

	install1 := installSilently(t, setup)

	// The data of the app: a file, and a junction to a folder the user works in.
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(project, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "kept.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(data, "project")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, project).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v\n%s", err, out)
	}

	// A silent uninstall keeps the data.
	uninstall(t, install1)
	if _, err := os.Stat(filepath.Join(data, "kept.txt")); err != nil {
		t.Fatalf("a silent uninstall deleted the data: %v", err)
	}

	// /PURGE deletes it, and the link goes without its target.
	install2 := installSilently(t, setup)
	uninstall(t, install2, "/PURGE")
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Errorf("/PURGE left %s (%v)", data, err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("/PURGE deleted the folder the junction points to: %v", err)
	}
}

// TestWindowsInstallerUserDataLinkOnTheWay checks a link on the way to a
// listed folder: the app's data folder is a junction to a folder the user
// works in, and the purge lists a folder under it. Test-Path and GetAttributes
// would follow the junction, so the purge must refuse it: nothing under the
// junction may be deleted, and the junction stays.
func TestWindowsInstallerUserDataLinkOnTheWay(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	const folder = "Setup Test Linked"
	setup, _ := buildUserDataInstaller(t, []string{`$APPDATA\` + folder + `\inner`})
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Fatal("APPDATA is not set")
	}
	data := filepath.Join(appData, folder)
	if _, err := os.Stat(data); err == nil {
		t.Fatalf("%s exists: the test would use it", data)
	}
	// The folder the user works in, with the folder the purge lists under it.
	project := filepath.Join(t.TempDir(), "project")
	sentinel := filepath.Join(project, "inner", "sentinel.txt")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The app's data folder is a junction to the project.
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", data, project).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v\n%s", err, out)
	}
	// os.Remove takes the link alone: it does not follow it.
	t.Cleanup(func() { os.Remove(data) })

	install := installSilently(t, setup)
	uninstall(t, install, "/PURGE")
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("/PURGE deleted through a link on the way to the listed folder: %v", err)
	}
	if _, err := os.Stat(data); err != nil {
		t.Errorf("/PURGE removed the link on the way to the listed folder: %v", err)
	}
}
