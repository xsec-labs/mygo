package main

import (
	"fmt"
	"strings"
	"unicode"
)

// The uninstaller of a Windows installer deletes the data of the app only
// when the user asks, for the folders windows.userData lists under $APPDATA
// or $LOCALAPPDATA. An interactive uninstall shows a checkbox for it, not
// checked by default; a silent one (/S) keeps the data unless it is given
// /PURGE, which deletes it without asking.
//
// The folders are deleted by PowerShell rather than by RMDir /r. An app can
// link a folder it works in (a junction into a project, for instance) into
// its own data, and that link has to go without its target: PowerShell
// removes each link as a link and never descends into it.
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
	call      string // in the uninstall section, before the app's folder goes
	functions string // after the uninstall section
	script    string // the PowerShell script the uninstaller runs, as purgeScriptName
}

// userDataUninstall returns the parts of the installer script for
// windows.userData.
func userDataUninstall(c *Config) (userDataParts, error) {
	if len(c.Windows.UserData) == 0 {
		return userDataParts{}, nil
	}
	var shown, purge strings.Builder
	for _, entry := range c.Windows.UserData {
		f, err := parseUserData(entry)
		if err != nil {
			return userDataParts{}, fmt.Errorf("windows.userData: %w", err)
		}
		// NSIS expands $APPDATA and $LOCALAPPDATA in the page's text, so
		// the page shows where the data is.
		fmt.Fprintf(&shown, `$\r$\n    $%s\%s`, f.root, f.rel)
		fmt.Fprintf(&purge, "Remove-Top $env:%s %s\n", f.root, psString(f.rel))
	}
	label := `"` + nsisEscape("Delete the data of "+c.Name+" on this computer as well? It is kept in these folders:") +
		shown.String() + `$\r$\n$\r$\n` + nsisEscape("Folders you work in are not deleted.") + `"`
	// The page's text is one NSIS string, and its folders expand to about
	// 60 characters each: a page too long for NSIS is refused, not cut.
	if len(label)+60*len(c.Windows.UserData) > nsisMaxString {
		return userDataParts{}, fmt.Errorf("windows.userData: the folders are too long for the uninstaller's page")
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
		page: "UninstPage custom un.UserDataPage un.UserDataLeave\n",
		call: "  ${If} $DeleteUserData == 1\n    Call un.DeleteUserData\n  ${EndIf}\n",
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
  ${NSD_CreateLabel} 0 0 100% 80u ` + label + `
  Pop $0
  ${NSD_CreateCheckbox} 0 88u 100% 12u "Also delete my data. This cannot be undone."
  Pop $UserDataCheckbox
  nsDialogs::Show
FunctionEnd

Function un.UserDataLeave
  ${NSD_GetState} $UserDataCheckbox $0
  ${If} $0 == ${BST_CHECKED}
    StrCpy $DeleteUserData 1
  ${EndIf}
FunctionEnd

Function un.DeleteUserData
  nsExec::Exec '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "$INSTDIR\` + purgeScriptName + `"'
  Pop $0
  ${If} $0 != "0"
    IfSilent +2 0
    MessageBox MB_OK|MB_ICONEXCLAMATION "Some of the data of ` + nsisEscape(c.Name) + ` could not be deleted. Close the app and delete the rest by hand."
  ${EndIf}
FunctionEnd
`,
		script: purgeScriptHead + purge.String() + "if ($script:failed -gt 0) { exit 1 }\n",
	}, nil
}

// purgeScriptHead defines the PowerShell functions of the purge. It runs
// on Windows PowerShell 5.1, which every supported Windows has. Each entry
// is removed on its own: a failure is counted and reported, and the rest
// still goes.
const purgeScriptHead = `$ErrorActionPreference = 'Stop'
$script:failed = 0
function Has([IO.FileAttributes]$a, [IO.FileAttributes]$f) { ($a -band $f) -eq $f }
function Remove-Entry([string]$p) {
  try {
    $a = [IO.File]::GetAttributes($p)
    if (Has $a ([IO.FileAttributes]::Directory)) {
      if (-not (Has $a ([IO.FileAttributes]::ReparsePoint))) {
        foreach ($c in [IO.Directory]::GetFileSystemEntries($p)) { Remove-Entry $c }
      }
      [IO.Directory]::Delete($p, $false)
    } else {
      if (Has $a ([IO.FileAttributes]::ReadOnly)) { [IO.File]::SetAttributes($p, [IO.FileAttributes]::Normal) }
      [IO.File]::Delete($p)
    }
  } catch {
    $script:failed++
    [Console]::Error.WriteLine($p + ': ' + $_.Exception.Message)
  }
}
function Remove-Top([string]$root, [string]$rel) {
  try {
    $p = Join-Path $root $rel
    if (Test-Path -LiteralPath $p) { Remove-Entry $p }
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
