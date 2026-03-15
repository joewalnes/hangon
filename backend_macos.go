//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MacOSBackendImpl interacts with macOS desktop apps via AppleScript and Accessibility APIs.
type MacOSBackendImpl struct {
	appName string
	output  *RingBuffer
	done    chan struct{}
	running bool
}

func NewMacOSBackend(appName string) Backend {
	return &MacOSBackendImpl{
		appName: appName,
		output:  NewRingBuffer(defaultBufSize),
		done:    make(chan struct{}),
	}
}

func (mb *MacOSBackendImpl) Start() error {
	// Launch the app.
	script := fmt.Sprintf(`tell application %q to activate`, mb.appName)
	if err := runOsascript(script); err != nil {
		return fmt.Errorf("launch %s: %w", mb.appName, err)
	}
	mb.running = true

	// Give the app a moment to come to foreground.
	time.Sleep(1 * time.Second)
	return nil
}

func (mb *MacOSBackendImpl) Send(data []byte) error {
	// For macOS, "send" types text using System Events keystroke.
	return mb.TypeText(string(data))
}

func (mb *MacOSBackendImpl) Output() *RingBuffer {
	return mb.output
}

func (mb *MacOSBackendImpl) Stderr() *RingBuffer {
	return nil
}

func (mb *MacOSBackendImpl) Screen() (string, error) {
	return "", ErrNotSupported
}

func (mb *MacOSBackendImpl) SendKeys(keys string) error {
	for _, key := range strings.Fields(keys) {
		if err := mb.sendMacKey(key); err != nil {
			return err
		}
	}
	return nil
}

func (mb *MacOSBackendImpl) Alive() bool {
	script := fmt.Sprintf(`tell application "System Events" to (name of processes) contains %q`, mb.appName)
	out, err := runOsascriptOutput(script)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

func (mb *MacOSBackendImpl) Wait() (int, error) {
	// Poll until app quits.
	for mb.Alive() {
		time.Sleep(1 * time.Second)
	}
	close(mb.done)
	return 0, nil
}

func (mb *MacOSBackendImpl) TargetPID() int {
	script := fmt.Sprintf(`tell application "System Events" to unix id of (first process whose name is %q)`, mb.appName)
	out, err := runOsascriptOutput(script)
	if err != nil {
		return 0
	}
	var pid int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &pid)
	return pid
}

func (mb *MacOSBackendImpl) Close() error {
	script := fmt.Sprintf(`tell application %q to quit`, mb.appName)
	runOsascript(script)
	return nil
}

// --- MacOSBackend interface ---

func (mb *MacOSBackendImpl) AxTree() (string, error) {
	script := fmt.Sprintf(`
tell application "System Events"
	tell process %q
		set output to ""
		set output to output & "Window: " & (name of front window) & "\n"
		set allElements to entire contents of front window
		repeat with elem in allElements
			try
				set elemRole to role of elem
				set elemDesc to description of elem
				set elemVal to ""
				try
					set elemVal to value of elem
				end try
				set output to output & elemRole & ": " & elemDesc
				if elemVal is not "" then
					set output to output & " [" & elemVal & "]"
				end if
				set output to output & "\n"
			end try
		end repeat
		return output
	end tell
end tell`, mb.appName)

	out, err := runOsascriptOutput(script)
	if err != nil {
		return "", fmt.Errorf("ax-tree: %w", err)
	}
	mb.output.Write([]byte(out))
	return out, nil
}

func (mb *MacOSBackendImpl) AxFind(role, name string) (string, error) {
	conditions := []string{}
	if role != "" {
		conditions = append(conditions, fmt.Sprintf(`role of elem is %q`, role))
	}
	if name != "" {
		conditions = append(conditions, fmt.Sprintf(`description of elem contains %q`, name))
	}
	if len(conditions) == 0 {
		return "", fmt.Errorf("ax-find requires --role or --name")
	}

	script := fmt.Sprintf(`
tell application "System Events"
	tell process %q
		set output to ""
		set allElements to entire contents of front window
		repeat with elem in allElements
			try
				if %s then
					set elemRole to role of elem
					set elemDesc to description of elem
					set output to output & elemRole & ": " & elemDesc & "\n"
				end if
			end try
		end repeat
		return output
	end tell
end tell`, mb.appName, strings.Join(conditions, " and "))

	out, err := runOsascriptOutput(script)
	if err != nil {
		return "", fmt.Errorf("ax-find: %w", err)
	}
	return out, nil
}

func (mb *MacOSBackendImpl) Click(element string) error {
	script := fmt.Sprintf(`
tell application "System Events"
	tell process %q
		try
			click (first UI element of front window whose description contains %q)
		on error
			click (first button of front window whose description contains %q)
		end try
	end tell
end tell`, mb.appName, element, element)

	if err := runOsascript(script); err != nil {
		return fmt.Errorf("click %q: %w", element, err)
	}
	return nil
}

func (mb *MacOSBackendImpl) TypeText(text string) error {
	// Ensure app is frontmost.
	activateScript := fmt.Sprintf(`tell application %q to activate`, mb.appName)
	runOsascript(activateScript)
	time.Sleep(200 * time.Millisecond)

	script := fmt.Sprintf(`tell application "System Events" to keystroke %q`, text)
	if err := runOsascript(script); err != nil {
		return fmt.Errorf("type: %w", err)
	}
	return nil
}

func (mb *MacOSBackendImpl) Screenshot(file string) (string, error) {
	if file == "" {
		file = "screenshot.png"
	}

	// Get window ID for targeted capture.
	pid := mb.TargetPID()
	if pid > 0 {
		// Try to get the window ID for this process.
		script := fmt.Sprintf(`
tell application "System Events"
	tell process %q
		set wID to id of front window
		return wID
	end tell
end tell`, mb.appName)
		if out, err := runOsascriptOutput(script); err == nil {
			windowID := strings.TrimSpace(out)
			if windowID != "" {
				cmd := exec.Command("screencapture", "-l", windowID, file)
				if err := cmd.Run(); err == nil {
					return file, nil
				}
			}
		}
	}

	// Fallback: capture frontmost window.
	cmd := exec.Command("screencapture", "-w", file)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("screenshot: %w", err)
	}
	return file, nil
}

func (mb *MacOSBackendImpl) sendMacKey(key string) error {
	key = strings.ToLower(key)

	// Map key names to AppleScript key codes.
	switch key {
	case "enter", "return":
		return runOsascript(`tell application "System Events" to key code 36`)
	case "tab":
		return runOsascript(`tell application "System Events" to key code 48`)
	case "escape", "esc":
		return runOsascript(`tell application "System Events" to key code 53`)
	case "backspace", "delete":
		return runOsascript(`tell application "System Events" to key code 51`)
	case "up":
		return runOsascript(`tell application "System Events" to key code 126`)
	case "down":
		return runOsascript(`tell application "System Events" to key code 125`)
	case "left":
		return runOsascript(`tell application "System Events" to key code 123`)
	case "right":
		return runOsascript(`tell application "System Events" to key code 124`)
	case "space":
		return runOsascript(`tell application "System Events" to keystroke " "`)
	}

	// Handle ctrl-X combinations.
	if strings.HasPrefix(key, "ctrl-") {
		char := strings.TrimPrefix(key, "ctrl-")
		script := fmt.Sprintf(`tell application "System Events" to keystroke %q using control down`, char)
		return runOsascript(script)
	}
	// Handle cmd-X combinations.
	if strings.HasPrefix(key, "cmd-") {
		char := strings.TrimPrefix(key, "cmd-")
		script := fmt.Sprintf(`tell application "System Events" to keystroke %q using command down`, char)
		return runOsascript(script)
	}

	return fmt.Errorf("unknown key: %s", key)
}

// --- osascript helpers ---

func runOsascript(script string) error {
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

func runOsascriptOutput(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	return string(out), err
}
