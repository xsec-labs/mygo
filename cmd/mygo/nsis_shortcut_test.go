package main

import "testing"

// TestDesktopShortcutLangStrings checks the finish page's checkbox text in each
// language of the installer: the language's own text, and the English one for
// a language without a text of its own.
func TestDesktopShortcutLangStrings(t *testing.T) {
	want := "LangString DesktopShortcutText ${LANG_ENGLISH} \"Create a desktop shortcut\"\n" +
		"LangString DesktopShortcutText ${LANG_SIMPCHINESE} \"创建桌面快捷方式\"\n" +
		"LangString DesktopShortcutText ${LANG_GERMAN} \"Create a desktop shortcut\"\n"
	if got := desktopShortcutLangStrings([]string{"English", "SimpChinese", "German"}); got != want {
		t.Errorf("desktopShortcutLangStrings =\n%s\nwant\n%s", got, want)
	}
}
