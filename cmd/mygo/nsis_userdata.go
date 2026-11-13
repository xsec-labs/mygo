package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The uninstaller of a Windows installer deletes the data of the app only
// when the user asks, for the folders windows.userData lists under $APPDATA
// or $LOCALAPPDATA. An interactive uninstall shows a checkbox for it, not
// checked by default; a silent one (/S) keeps the data unless it is given
// /PURGE, which deletes it without asking.
//
// The deletion is all or nothing while no file is in use. The purge checks
// every listed folder first, and deletes nothing when a file in them cannot
// be opened for deletion or a folder cannot be checked; only then does it
// delete. A file that comes into use after the check can still leave part of
// the data behind.
//
// The folders are deleted by PowerShell rather than by RMDir /r. An app can
// link a folder it works in (a junction into a project, for instance) into
// its own data, and that link has to go without its target: PowerShell
// removes each link as a link and never descends into it. A link on the way
// to a listed folder is different: the purge refuses to delete under such a
// link at all, since a path through it would reach the link's target. The
// purge reads each folder's listing rather than testing paths, because
// Test-Path reads a link whose target is gone as absent, and such a link is
// removed like any other.
//
// The PowerShell script is a file the installer puts in the app's folder,
// not part of the uninstaller's command line. NSIS cuts a string at
// NSIS_MAX_STRLEN (1024 characters by default) when the uninstaller runs,
// and the script encoded for -EncodedCommand is longer than that: it was
// cut, PowerShell refused it, and the folders stayed.

// nsisMaxString is NSIS_MAX_STRLEN by default: NSIS cuts a longer string.
const nsisMaxString = 1024

// purgeScriptName is the PowerShell script that deletes the data. The
// installer puts it in the app's folder, and the uninstaller runs it before
// it removes that folder.
const purgeScriptName = "userdata-purge.ps1"

// Exit codes of the purge script. A purge that stops before it deletes
// anything exits with purgeNothingDeleted; one that fails while it deletes
// exits with 1.
const purgeNothingDeleted = 2

// userDataFolder is an entry of windows.userData: rel, a folder under the
// NSIS variable $root (APPDATA or LOCALAPPDATA), with \ separators.
type userDataFolder struct {
	root, rel string
}

// parseUserData checks an entry of windows.userData, a folder under
// $APPDATA or $LOCALAPPDATA such as "$APPDATA\My App" (/ separates too),
// and splits it.
func parseUserData(entry string) (userDataFolder, error) {
	e := strings.ReplaceAll(entry, "/", `\`)
	for _, root := range []string{"APPDATA", "LOCALAPPDATA"} {
		prefix := "$" + root + `\`
		if !strings.HasPrefix(e, prefix) {
			continue
		}
		rel := e[len(prefix):]
		for _, part := range strings.Split(rel, `\`) {
			if !validUserDataName(part) {
				return userDataFolder{}, fmt.Errorf("%q: %q is not a folder name", entry, part)
			}
		}
		return userDataFolder{root: root, rel: rel}, nil
	}
	return userDataFolder{}, fmt.Errorf(`%q is not a folder under $APPDATA or $LOCALAPPDATA, as "$APPDATA\My App"`, entry)
}

// validUserDataName reports whether part can name a folder on Windows and
// is safe in the installer: not empty, . or .., without a leading space or
// a trailing dot or space, and without the characters Windows forbids in
// names, or $ and " which NSIS reads as its own syntax.
func validUserDataName(part string) bool {
	if part == "" || part == "." || part == ".." || strings.TrimLeft(part, " ") != part || strings.TrimRight(part, ". ") != part {
		return false
	}
	return !strings.ContainsAny(part, `"$:*?<>|`) && strings.IndexFunc(part, unicode.IsControl) < 0
}

// userDataParts are the parts of the installer script that let the
// uninstaller delete the data. Each is empty without windows.userData.
type userDataParts struct {
	head      string // after MUI2.nsh: includes and variables
	page      string // between the confirm page and the progress page
	call      string // first in the uninstall section, before anything is removed
	texts     string // after the languages: the page's and the messages' texts, per language
	functions string // after the uninstall section
	script    string // the PowerShell script the uninstaller runs, as purgeScriptName
}

// uninstallText is the text of the uninstaller's page and of its messages in
// one language. The formats take the app's name where %s is.
type uninstallText struct {
	question   string // the question the page asks, before the note
	kept       string // what comes before the folders
	notDeleted string // what comes after the folders
	checkbox   string // the checkbox that asks for the deletion
	inUse      string // the message when the purge deleted nothing
	failed     string // the message when the purge failed while it deleted
}

// uninstallTexts are the texts of the uninstaller by the name of the NSIS
// language file. A language without a table of its own gets the English one.
var uninstallTexts = map[string]uninstallText{
	"English": {
		question:   "Delete the data of %s on this computer as well?",
		kept:       "It is kept in these folders:",
		notDeleted: "Folders you work in are not deleted.",
		checkbox:   "Also delete my data. This cannot be undone.",
		inUse:      "The uninstall was cancelled and no data was deleted: some of the data of %s is in use or cannot be checked. Close the app and run the uninstaller again.",
		failed:     "The uninstall was cancelled: some of the data of %s could not be deleted. Close the app and run the uninstaller again.",
	},
	"SimpChinese": {
		question:   "同时删除这台电脑上 %s 的数据吗？",
		kept:       "如果不删除，数据保存在以下文件夹中：",
		notDeleted: "你工作的文件夹不会被删除。",
		checkbox:   "同时删除我的数据。此操作无法撤销。",
		inUse:      "卸载已取消，没有删除任何数据：%s 的部分数据正在被占用，或无法检查。请关闭应用后再次运行卸载程序。",
		failed:     "卸载已取消：%s 的部分数据无法删除。请关闭应用后再次运行卸载程序。",
	},
}

// uninstallTextFor returns the text of the uninstaller in the language of an
// NSIS language file, such as "SimpChinese".
func uninstallTextFor(nlfid string) uninstallText {
	if t, ok := uninstallTexts[nlfid]; ok {
		return t
	}
	return uninstallTexts["English"]
}

// maxUserDataNote is the longest windows.userDataNote, in characters: the
// uninstaller's page has room for a line or two of it.
const maxUserDataNote = 120

// checkUserDataNote reports whether the note of windows.userDataNote can go on
// the uninstaller's page: it needs windows.userData, it is one line of text,
// and it is no longer than maxUserDataNote characters.
func checkUserDataNote(w *Windows) error {
	note := w.UserDataNote
	if note == "" {
		return nil
	}
	if len(w.UserData) == 0 {
		return fmt.Errorf("windows.userDataNote needs windows.userData")
	}
	if strings.IndexFunc(note, unicode.IsControl) >= 0 {
		return fmt.Errorf("windows.userDataNote is one line of text")
	}
	if utf8.RuneCountInString(note) > maxUserDataNote {
		return fmt.Errorf("windows.userDataNote is too long: at most %d characters", maxUserDataNote)
	}
	return nil
}

// userDataUninstall returns the parts of the installer script for
// windows.userData, whose texts are in each of the languages nlfids names.
func userDataUninstall(c *Config, nlfids []string) (userDataParts, error) {
	if err := checkUserDataNote(&c.Windows); err != nil {
		return userDataParts{}, err
	}
	if len(c.Windows.UserData) == 0 {
		return userDataParts{}, nil
	}
	var shown, check, purge strings.Builder
	for _, entry := range c.Windows.UserData {
		f, err := parseUserData(entry)
		if err != nil {
			return userDataParts{}, fmt.Errorf("windows.userData: %w", err)
		}
		// NSIS expands $APPDATA and $LOCALAPPDATA in the page's text, so
		// the page shows where the data is.
		fmt.Fprintf(&shown, `$\r$\n    $%s\%s`, f.root, f.rel)
		fmt.Fprintf(&check, "Check-Top $env:%s %s\n", f.root, psString(f.rel))
		fmt.Fprintf(&purge, "Remove-Top $env:%s %s\n", f.root, psString(f.rel))
	}
	var texts strings.Builder
	for _, nlfid := range nlfids {
		t := uninstallTextFor(nlfid)
		// The question, then the note on its own line if there is one, then
		// the folders.
		sep, noteLine := " ", ""
		if note := c.Windows.UserDataNote; note != "" {
			sep, noteLine = `$\r$\n`, nsisEscape(note)+`$\r$\n`
		}
		label := nsisEscape(fmt.Sprintf(t.question, c.Name)) + sep + noteLine + nsisEscape(t.kept) +
			shown.String() + `$\r$\n$\r$\n` + nsisEscape(t.notDeleted)
		// The page's text is one NSIS string, and its folders expand to about
		// 60 characters each: a page too long for NSIS is refused, not cut.
		if len(label)+60*len(c.Windows.UserData) > nsisMaxString {
			return userDataParts{}, fmt.Errorf("windows.userData: the folders are too long for the uninstaller's page")
		}
		id := strings.ToUpper(nlfid)
		fmt.Fprintf(&texts, "LangString UD_Label ${LANG_%s} \"%s\"\n", id, label)
		fmt.Fprintf(&texts, "LangString UD_Checkbox ${LANG_%s} %s\n", id, nsisString(t.checkbox))
		fmt.Fprintf(&texts, "LangString UD_InUse ${LANG_%s} %s\n", id, nsisString(fmt.Sprintf(t.inUse, c.Name)))
		fmt.Fprintf(&texts, "LangString UD_Failed ${LANG_%s} %s\n", id, nsisString(fmt.Sprintf(t.failed, c.Name)))
	}
	return userDataParts{
		head: `!include "FileFunc.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!insertmacro un.GetParameters
!insertmacro un.GetOptions
Var DeleteUserData
Var UserDataCheckbox
`,
		page:  "UninstPage custom un.UserDataPage un.UserDataLeave\n",
		call:  "  ; The data goes first: if any of it is in use, none is deleted and nothing else is removed.\n  ${If} $DeleteUserData == 1\n    Call un.DeleteUserData\n  ${EndIf}\n",
		texts: texts.String(),
		functions: `Function un.onInit
  StrCpy $DeleteUserData 0
  ${un.GetParameters} $R0
  ${un.GetOptions} $R0 "/PURGE" $R1
  IfErrors +2 0
  StrCpy $DeleteUserData 1
FunctionEnd

; The page asks unless the uninstall is silent or /PURGE is given.
Function un.UserDataPage
  IfSilent 0 +2
  Abort
  ${If} $DeleteUserData == 1
    Abort
  ${EndIf}
  nsDialogs::Create 1018
  Pop $0
  ${If} $0 == error
    Abort
  ${EndIf}
  ${NSD_CreateLabel} 0 0 100% 80u "$(UD_Label)"
  Pop $0
  ${NSD_CreateCheckbox} 0 88u 100% 12u "$(UD_Checkbox)"
  Pop $UserDataCheckbox
  nsDialogs::Show
FunctionEnd

Function un.UserDataLeave
  ${NSD_GetState} $UserDataCheckbox $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $DeleteUserData 1
  ${EndIf}
FunctionEnd

; A purge that fails stops the uninstall before anything else is removed, so the
; uninstaller, its entry and the shortcuts stay, and the uninstall can run again
; once the app is closed. The purge exits with ` + fmt.Sprint(purgeNothingDeleted) + ` when it deleted nothing.
Function un.DeleteUserData
  nsExec::Exec '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\` + purgeScriptName + `"'
  Pop $0
  ${If} $0 == "` + fmt.Sprint(purgeNothingDeleted) + `"
    IfSilent +2 0
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(UD_InUse)"
    SetErrorLevel 1
    Quit
  ${EndIf}
  ${If} $0 != "0"
    IfSilent +2 0
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(UD_Failed)"
    SetErrorLevel 1
    Quit
  ${EndIf}
FunctionEnd
`,
		script: purgeScriptHead + check.String() + "if ($script:failed -gt 0) { exit " + fmt.Sprint(purgeNothingDeleted) + " }\n" +
			purge.String() + "if ($script:failed -gt 0) { exit 1 }\n",
	}, nil
}

// purgeScriptHead defines the PowerShell functions of the purge. It runs
// on Windows PowerShell 5.1, which every supported Windows has. The folders
// are checked first, and nothing is deleted if a check fails. Then each entry
// is removed on its own: a failure is counted and reported, and the rest still
// goes.
const purgeScriptHead = `$ErrorActionPreference = 'Stop'
$script:failed = 0
function Has([IO.FileAttributes]$a, [IO.FileAttributes]$f) { ($a -band $f) -eq $f }
function Find-Entry([string]$dir, [string]$name) {
  # The listing of the folder shows a link whose target is gone as an entry;
  # Test-Path reads such a link as absent. Only a folder that is not there has
  # no entries. Any other error, such as access denied, is raised: Directory.Exists
  # answers false for some errors too, so it cannot tell a missing folder apart.
  try {
    $found = (New-Object System.IO.DirectoryInfo -ArgumentList $dir).GetFileSystemInfos($name)
  } catch [System.IO.DirectoryNotFoundException] {
    return $null
  }
  foreach ($e in $found) { return $e }
  return $null
}
function Find-Target([string]$root, [string]$rel) {
  # The entry the listed folder names, or $null when there is none. Each folder
  # above the listed one is found in its parent first. A link there stops the
  # purge of the folder below it, before anything under it is deleted. A link
  # whose target is gone holds nothing, so it stops nothing.
  $parts = $rel.Split('\')
  $dir = $root
  for ($i = 0; $i -lt $parts.Length - 1; $i++) {
    $e = Find-Entry $dir ($parts[$i])
    if ($null -eq $e) { return $null }
    if (Has ($e.Attributes) ([IO.FileAttributes]::ReparsePoint)) {
      if (-not [IO.Directory]::Exists($e.FullName)) { return $null }
      throw ('not following the link on the way to ' + $e.FullName)
    }
    $dir = $e.FullName
  }
  return (Find-Entry $dir ($parts[$parts.Length - 1]))
}
function Check-Entry($e) {
  # Checks that Remove-Entry can delete e, and deletes nothing. A file that
  # another process has open cannot be opened here, so the purge stops before
  # it deletes anything. A link is not opened: its target is not part of the data.
  try {
    if (Has ($e.Attributes) ([IO.FileAttributes]::Directory)) {
      if (-not (Has ($e.Attributes) ([IO.FileAttributes]::ReparsePoint))) {
        foreach ($c in (New-Object System.IO.DirectoryInfo -ArgumentList $e.FullName).GetFileSystemInfos()) { Check-Entry $c }
      }
    } elseif (-not (Has ($e.Attributes) ([IO.FileAttributes]::ReparsePoint))) {
      [IO.File]::Open($e.FullName, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::None).Close()
    }
  } catch {
    $script:failed++
    [Console]::Error.WriteLine($e.FullName + ': ' + $_.Exception.Message)
  }
}
function Check-Top([string]$root, [string]$rel) {
  try {
    $last = Find-Target $root $rel
    if ($null -ne $last) { Check-Entry $last }
  } catch {
    $script:failed++
    [Console]::Error.WriteLine($_.Exception.Message)
  }
}
function Remove-Entry($e) {
  try {
    $p = $e.FullName
    if (Has ($e.Attributes) ([IO.FileAttributes]::Directory)) {
      if (-not (Has ($e.Attributes) ([IO.FileAttributes]::ReparsePoint))) {
        foreach ($c in (New-Object System.IO.DirectoryInfo -ArgumentList $p).GetFileSystemInfos()) { Remove-Entry $c }
      }
      [IO.Directory]::Delete($p, $false)
    } else {
      if (Has ($e.Attributes) ([IO.FileAttributes]::ReadOnly)) { [IO.File]::SetAttributes($p, [IO.FileAttributes]::Normal) }
      [IO.File]::Delete($p)
    }
  } catch {
    $script:failed++
    [Console]::Error.WriteLine($e.FullName + ': ' + $_.Exception.Message)
  }
}
function Remove-Top([string]$root, [string]$rel) {
  try {
    $last = Find-Target $root $rel
    if ($null -ne $last) { Remove-Entry $last }
  } catch {
    $script:failed++
    [Console]::Error.WriteLine($_.Exception.Message)
  }
}
`

// psString quotes s as a PowerShell string.
func psString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
