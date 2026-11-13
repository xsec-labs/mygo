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
// given windows.userData, in English.
func userDataScript(t *testing.T, userData []string) userDataParts {
	t.Helper()
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = userData
	parts, err := userDataUninstall(c, []string{"English"})
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
	// The page shows where the data is: NSIS expands the variables in its text.
	if !strings.Contains(parts.texts, `$APPDATA\xSec Desktop`) || !strings.Contains(parts.texts, `$LOCALAPPDATA\XSec`) {
		t.Error("the page does not name the folders")
	}
	// The uninstaller runs the script the installer puts in the app's folder.
	if !strings.Contains(parts.functions, `-File "$INSTDIR\`+purgeScriptName+`"`) {
		t.Error("the uninstaller does not run the purge script")
	}
	// The purge removes each link in the folders without following it.
	for _, want := range []string{
		`Check-Top $env:APPDATA 'xSec Desktop'`,
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
	// Every folder is checked before anything is deleted: a failed check exits
	// before the first deletion, so nothing is deleted.
	checked, exit, removed := strings.LastIndex(parts.script, "\nCheck-Top "), strings.Index(parts.script, "exit 2"), strings.Index(parts.script, "\nRemove-Top ")
	if checked < 0 || exit < checked || removed < exit {
		t.Errorf("the purge script does not check every folder before it deletes: check at %d, exit at %d, delete at %d", checked, exit, removed)
	}
	// The check deletes and changes nothing: only Remove-Entry does.
	if checks := purgeScriptHead[:strings.Index(purgeScriptHead, "function Remove-Entry")]; strings.Contains(checks, "Delete") || strings.Contains(checks, "SetAttributes") {
		t.Error("the check deletes or changes files: only Remove-Entry may")
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
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = []string{`$APPDATA\xSec Desktop`, `$APPDATA\XSec`, `$LOCALAPPDATA\XSec`}
	parts, err := userDataUninstall(c, []string{"English", "SimpChinese"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{parts.head, parts.page, parts.call, parts.texts, parts.functions} {
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
	c.Windows.UserData = long
	if _, err := userDataUninstall(c, []string{"English"}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("userDataUninstall accepted folders that the page cannot show: %v", err)
	}
}

// TestUninstallTexts checks the texts of the uninstaller in each language: each
// has its text, and the ones that name the app keep the %s where it goes.
func TestUninstallTexts(t *testing.T) {
	for nlfid, text := range uninstallTexts {
		for name, s := range map[string]string{
			"question": text.question, "kept": text.kept, "notDeleted": text.notDeleted,
			"checkbox": text.checkbox, "inUse": text.inUse, "failed": text.failed,
		} {
			if s == "" {
				t.Errorf("%s: the %s text is empty", nlfid, name)
			}
		}
		for name, s := range map[string]string{"question": text.question, "inUse": text.inUse, "failed": text.failed} {
			if strings.Count(s, "%s") != 1 {
				t.Errorf("%s: the %s text must name the app once, with %%s", nlfid, name)
			}
		}
	}
	if got := uninstallTextFor("German"); got != uninstallTexts["English"] {
		t.Error("a language without a table of its own does not get the English texts")
	}
}

// TestUserDataTextsByLanguage checks that the uninstaller's texts come in each
// language of the installer, under the name of its NSIS language file.
func TestUserDataTextsByLanguage(t *testing.T) {
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = []string{`$APPDATA\xSec Desktop`}
	c.Windows.UserDataNote = "你的保险库和云端登录凭据将一并删除。"
	parts, err := userDataUninstall(c, []string{"English", "SimpChinese"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`LangString UD_Label ${LANG_SIMPCHINESE} "同时删除这台电脑上 Userdata App 的数据吗？$\r$\n你的保险库和云端登录凭据将一并删除。$\r$\n如果不删除，数据保存在以下文件夹中：`,
		`LangString UD_Checkbox ${LANG_SIMPCHINESE} "同时删除我的数据。此操作无法撤销。"`,
		`LangString UD_Checkbox ${LANG_ENGLISH} "Also delete my data. This cannot be undone."`,
	} {
		if !strings.Contains(parts.texts, want) {
			t.Errorf("the texts lack %q", want)
		}
	}
}

// buildWindowsInstaller builds the installer of an app named Setup Test, whose
// windows configuration is the JSON members given after its signing command.
// It returns the setup program, and the folder the build wrote its files to.
func buildWindowsInstaller(t *testing.T, members string) (setup, work string) {
	t.Helper()
	dir := testModule(t, map[string]string{
		"main.go":              "package main\n\nfunc main() {}\n",
		"mygo.json":            `{"name": "Setup Test", "identifier": "com.example.setuptest", "version": "2.0.0", "windows": {"signCommand": "echo %1>>signed.txt", ` + members + `}}`,
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

// buildUserDataInstaller builds the installer of buildWindowsInstaller with
// windows.userData as userData, and windows.userDataNote as note when one is
// given.
func buildUserDataInstaller(t *testing.T, userData []string, note ...string) (setup, work string) {
	t.Helper()
	list, err := json.Marshal(userData)
	if err != nil {
		t.Fatal(err)
	}
	userDataJSON := `"userData": ` + string(list)
	if len(note) > 0 {
		text, err := json.Marshal(note[0])
		if err != nil {
			t.Fatal(err)
		}
		userDataJSON += `, "userDataNote": ` + string(text)
	}
	return buildWindowsInstaller(t, userDataJSON)
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

// TestWindowsInstallerLanguage checks that windows.language, for an app without
// translations, puts the installer and its uninstaller in that language, and
// that the uninstaller's texts come in it.
func TestWindowsInstallerLanguage(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	_, work := buildWindowsInstaller(t, `"language": "zh-Hans", "userData": ["$APPDATA\\Setup Test Language"]`)
	script, err := os.ReadFile(filepath.Join(work, "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`!insertmacro MUI_LANGUAGE "SimpChinese"`,
		`LangString UD_Checkbox ${LANG_SIMPCHINESE} "同时删除我的数据。此操作无法撤销。"`,
		`LangString DesktopShortcutText ${LANG_SIMPCHINESE} "创建桌面快捷方式"`,
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("the installer lacks %q", want)
		}
	}
	if strings.Contains(string(script), `MUI_LANGUAGE "English"`) {
		t.Error("the installer has English as well as the language of windows.language")
	}
	if strings.Contains(string(script), "Create a desktop shortcut") {
		t.Error("the finish page's checkbox is in English in a Chinese installer")
	}
}

// TestWindowsLanguageConfigIsChecked checks that loadConfig refuses a
// windows.language NSIS has no language file of, on every platform.
func TestWindowsLanguageConfigIsChecked(t *testing.T) {
	for _, tc := range []struct {
		lang string
		ok   bool
	}{{"zh-Hans", true}, {"zh-hans", true}, {"en", true}, {"xx-YY", false}} {
		dir := testModule(t, map[string]string{
			"main.go":   "package main\n\nfunc main() {}\n",
			"mygo.json": `{"name": "Userdata", "identifier": "com.example.userdata", "version": "1.0.0", "windows": {"language": "` + tc.lang + `"}}`,
		})
		_, err := loadConfig(dir)
		if (err == nil) != tc.ok {
			t.Errorf("windows.language %q: loadConfig = %v", tc.lang, err)
		} else if err != nil && !strings.Contains(err.Error(), "windows.language") {
			t.Errorf("windows.language %q: the error does not name the setting: %v", tc.lang, err)
		}
	}
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
// junction may be deleted, and the junction stays: the uninstall stops there.
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
	if err := uninstallInPlace(t, install, "/PURGE"); err == nil {
		t.Error("the uninstall reported success although the purge refused the link")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("/PURGE deleted through a link on the way to the listed folder: %v", err)
	}
	if _, err := os.Lstat(data); err != nil {
		t.Errorf("/PURGE removed the link on the way to the listed folder: %v", err)
	}
	// Without the link, the same uninstall runs to the end.
	if err := os.Remove(data); err != nil {
		t.Fatal(err)
	}
	uninstall(t, install, "/PURGE")
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the retried /PURGE deleted through the link: %v", err)
	}
}

// uninstallInPlace runs the uninstaller of the app installed in dir silently,
// with the extra arguments, and returns its error. It runs in place, from dir,
// not from a copy, so the call waits until the uninstall is over and its exit
// code is the uninstaller's own.
func uninstallInPlace(t *testing.T, dir string, args ...string) error {
	t.Helper()
	args = append(append([]string{"/S"}, args...), "_?="+dir)
	out, err := exec.Command(filepath.Join(dir, "Uninstall.exe"), args...).CombinedOutput()
	t.Logf("uninstaller: %s", out)
	return err
}

// TestWindowsInstallerUserDataLocked checks a file of the data that is in use:
// the purge cannot delete it, so the uninstall deletes nothing and stops. The
// folder listed first, which has no file in use, stays too; the app and its
// uninstaller stay; and the same uninstall runs to the end once the file is
// free.
func TestWindowsInstallerUserDataLocked(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	const folder, free = "Setup Test Locked", "Setup Test Locked Free"
	setup, _ := buildUserDataInstaller(t, []string{`$APPDATA\` + free, `$APPDATA\` + folder})
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Fatal("APPDATA is not set")
	}
	data := filepath.Join(appData, folder)
	freeData := filepath.Join(appData, free)
	if _, err := os.Stat(data); err == nil {
		t.Fatalf("%s exists: the test would delete it", data)
	}
	if _, err := os.Stat(freeData); err == nil {
		t.Fatalf("%s exists: the test would delete it", freeData)
	}
	t.Cleanup(func() { os.RemoveAll(data) })
	t.Cleanup(func() { os.RemoveAll(freeData) })

	install := installSilently(t, setup)
	if err := os.MkdirAll(freeData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(freeData, "free.txt"), []byte("not in use"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(data, "locked.txt")
	if err := os.WriteFile(locked, []byte("in use"), 0o644); err != nil {
		t.Fatal(err)
	}
	// os.Open does not share deletion, so the purge cannot delete the file
	// while the handle is open.
	f, err := os.Open(locked)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	if err := uninstallInPlace(t, install, "/PURGE"); err == nil {
		t.Error("the uninstall reported success although the purge failed")
	}
	if _, err := os.Stat(locked); err != nil {
		t.Errorf("the purge deleted the file in use: %v", err)
	}
	// All or nothing: the folder listed first has nothing in use, and it stays.
	if _, err := os.Stat(filepath.Join(freeData, "free.txt")); err != nil {
		t.Errorf("the purge deleted a folder although a file in use stopped it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(install, "Uninstall.exe")); err != nil {
		t.Errorf("the uninstall stopped, but its uninstaller is gone: %v", err)
	}

	// Once the file is free, the same uninstall runs to the end.
	f.Close()
	uninstall(t, install, "/PURGE")
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Errorf("the retried /PURGE left %s (%v)", data, err)
	}
	if _, err := os.Stat(freeData); !os.IsNotExist(err) {
		t.Errorf("the retried /PURGE left %s (%v)", freeData, err)
	}
}

// TestWindowsInstallerUserDataDanglingLink checks a folder of the data that is
// a junction whose target is gone. Test-Path reads such a link as absent, so
// the purge skipped it and left the junction behind while reporting success.
// The junction must go: its target is not there to keep, and it holds nothing.
func TestWindowsInstallerUserDataDanglingLink(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	const folder = "Setup Test Dangling"
	setup, _ := buildUserDataInstaller(t, []string{`$APPDATA\` + folder})
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Fatal("APPDATA is not set")
	}
	data := filepath.Join(appData, folder)
	if _, err := os.Lstat(data); err == nil {
		t.Fatalf("%s exists: the test would use it", data)
	}
	// A junction to a folder that is removed at once, so the junction dangles.
	target := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", data, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v\n%s", err, out)
	}
	// os.Remove takes the link alone: it does not follow it.
	t.Cleanup(func() { os.Remove(data) })
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("the junction does not dangle: %v", err)
	}
	if _, err := os.Lstat(data); err != nil {
		t.Fatalf("the junction is not there: %v", err)
	}

	install := installSilently(t, setup)
	uninstall(t, install, "/PURGE")
	if _, err := os.Lstat(data); !os.IsNotExist(err) {
		t.Errorf("/PURGE left the dangling junction %s (%v)", data, err)
	}
}

// TestPurgeFindEntryRaisesErrors checks that the purge reads a folder it cannot
// check as an error, not as a missing folder. The case here is a path that Windows
// does not allow: Directory.Exists answers false for it, so the old check skipped it
// and the purge reported success. Find-Entry must raise that error, and only a
// folder that is not there may read as absent. Access denied is not tested: on a
// device it did not reproduce the hidden error, since Directory.Exists answered true.
func TestPurgeFindEntryRaisesErrors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the purge script runs on Windows PowerShell")
	}
	dir := t.TempDir()
	script := purgeScriptHead + `
try { if ($null -eq (Find-Entry ` + psString(filepath.Join(dir, "missing")) + ` 'x')) { 'missing: null' } else { 'missing: entry' } } catch { 'missing: threw' }
try { if ($null -eq (Find-Entry ` + psString(filepath.Join(dir, "no<such")) + ` 'x')) { 'invalid: null' } else { 'invalid: entry' } } catch { 'invalid: threw' }
`
	path := filepath.Join(dir, "find-entry.ps1")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	out, err := exec.Command(powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path).CombinedOutput()
	if err != nil {
		t.Fatalf("powershell: %v\n%s", err, out)
	}
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			got = append(got, line)
		}
	}
	if want := "missing: null|invalid: threw"; strings.Join(got, "|") != want {
		t.Errorf("Find-Entry answers %q, want %q", got, want)
	}
}

// TestUserDataNote checks the line the uninstaller's page shows about the
// folders. It sits between the question and the folders, escaped for NSIS;
// it needs windows.userData, and it is one line of text.
func TestUserDataNote(t *testing.T) {
	c := &Config{Name: "Userdata App"}
	c.Windows.UserData = []string{`$APPDATA\xSec Desktop`}
	c.Windows.UserDataNote = `Your $vault and your "cloud" login credentials will be deleted.`
	parts, err := userDataUninstall(c, []string{"English"})
	if err != nil {
		t.Fatal(err)
	}
	want := `as well?$\r$\n` + nsisEscape(c.Windows.UserDataNote) + `$\r$\nIt is kept in these folders:`
	if !strings.Contains(parts.texts, want) {
		t.Errorf("the page lacks the note between the question and the folders: %q", want)
	}
	c.Windows.UserData = nil
	if _, err := userDataUninstall(c, []string{"English"}); err == nil || !strings.Contains(err.Error(), "needs windows.userData") {
		t.Errorf("a note without windows.userData was accepted: %v", err)
	}
	c.Windows.UserData = []string{`$APPDATA\xSec Desktop`}
	c.Windows.UserDataNote = "two\nlines"
	if _, err := userDataUninstall(c, []string{"English"}); err == nil || !strings.Contains(err.Error(), "one line of text") {
		t.Errorf("a note of two lines was accepted: %v", err)
	}
	// A note of the longest length the page takes is accepted; a longer one is refused.
	c.Windows.UserDataNote = strings.Repeat("N", maxUserDataNote)
	if _, err := userDataUninstall(c, []string{"English"}); err != nil {
		t.Errorf("a note of %d characters was refused: %v", maxUserDataNote, err)
	}
	c.Windows.UserDataNote = strings.Repeat("N", maxUserDataNote+1)
	if _, err := userDataUninstall(c, []string{"English"}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("a note too long for the page was accepted: %v", err)
	}
}

// TestUserDataNoteInstaller checks that the note reaches the page of the
// installer that NSIS compiles.
func TestUserDataNoteInstaller(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles programs")
	}
	if tool, err := nsisCompiler(); err != nil {
		t.Fatal(err)
	} else if tool == "" {
		t.Skip("NSIS is not installed")
	}
	const note = "Your vault and your cloud login credentials will be deleted."
	_, work := buildUserDataInstaller(t, []string{`$APPDATA\Setup Test Note`}, note)
	script, err := os.ReadFile(filepath.Join(work, "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `as well?$\r$\n`+note+`$\r$\nIt is kept in these folders:`) {
		t.Fatal("the uninstaller's page lacks the note")
	}
}

// TestUserDataNoteConfigIsChecked checks that loadConfig refuses a note the
// uninstaller's page cannot show, on every platform, not only where NSIS runs.
func TestUserDataNoteConfigIsChecked(t *testing.T) {
	for _, tc := range []struct{ name, windows, want string }{
		{"without userData", `"userDataNote": "Your vault is deleted."`, "needs windows.userData"},
		{"two lines", `"userData": ["$APPDATA\\xSec"], "userDataNote": "one\ntwo"`, "one line of text"},
		{"too long", `"userData": ["$APPDATA\\xSec"], "userDataNote": "` + strings.Repeat("N", maxUserDataNote+1) + `"`, "too long"},
	} {
		dir := testModule(t, map[string]string{
			"main.go":   "package main\n\nfunc main() {}\n",
			"mygo.json": `{"name": "Userdata", "identifier": "com.example.userdata", "version": "1.0.0", "windows": {` + tc.windows + `}}`,
		})
		if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: loadConfig = %v, want an error with %q", tc.name, err, tc.want)
		}
	}
}
