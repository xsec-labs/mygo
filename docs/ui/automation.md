# Automation

`ui.Automate` drives a window that is open and showing a `ui.View`, from
outside its view function: it reads the elements the last frame shows,
sends the window the events its surface sends, and waits for what the
frames show. It is for tools that operate a running app, as the UI
automation server of a development build does; a view under test runs in a
[Tester](testing.md) instead.

```go
a, ok := ui.Automate(win) // win is the *mygo.Window showing the View
snap, err := a.Wait(ctx, func(s ui.AutomationSnapshot) bool {
	return slices.ContainsFunc(s.Nodes, func(n ui.AutomationNode) bool { return n.Label == "Projects" })
})
```

Every method is safe from any goroutine: the work runs on the main thread
and the call waits for it. `ui.AutomatedWindows` lists the open windows.

- **`Snapshot`** returns the elements of the last frame as assistive
  technology sees them (`AutomationNode`: ID, Role, Label, Value, Bounds,
  States, Actions), the focus, and `Frame`, the number of frames presented.
- **`Wait`** returns the first snapshot a condition accepts: the current one,
  or the first frame presented after that. It uses no timer; the condition
  runs once per frame, on the main thread, so it must be quick and pure. It
  ends with the context's error or `ErrAutomationClosed`.
- **`Inject`** sends events as the surface does (`AutomationInput`): `move`,
  `down`, `up`, `scroll`, `keydown`, `keyup`, `text`, `compose`, `command`,
  `focus`, `blur`, and `access`, the actions of assistive technology on an
  element by ID (press, focus, increment, setvalue, scrollintoview, ...).
  After each event it waits for the frame the event asked for, so a press is
  built before its release, as a Tester settles.
- **`ParseKey`** turns "Enter", "Escape", "F5" or "a" into a `Key`.

Events enter at the same place the platform's do, below every element and
above the operating system: no focus or permission of the desktop is needed,
and the same calls work on macOS, Windows and Linux. A screenshot is
`Window.CapturePage`.
