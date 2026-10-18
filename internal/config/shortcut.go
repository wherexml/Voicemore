package config

import (
	"errors"
	"strconv"
	"strings"
)

var ErrUnsafeShortcut = errors.New("use Command, Control, Option or Fn with a typing key, or choose a modifier/function key")

// ValidateShortcut is shared by recording, persistence and registration. Empty
// disables a binding. Existing modifier-only gestures remain supported; Shift
// alone never makes a typing/navigation key a safe global shortcut.
func ValidateShortcut(shortcut string) error {
	if shortcut == "" {
		return nil
	}
	parts := strings.Split(shortcut, "+")
	if strings.HasSuffix(shortcut, "++") {
		parts = append(parts[:len(parts)-2], "plus")
	}
	seen := map[string]bool{}
	strong, ordinary := false, ""
	for i, raw := range parts {
		p := strings.ToLower(strings.TrimSpace(raw))
		switch p {
		case "command", "cmd", "super", "meta", "win", "cmdorctrl", "commandorcontrol":
			p = "cmd"
		case "control":
			p = "ctrl"
		case "option", "altgr":
			p = "alt"
		}
		if p == "" || seen[p] {
			return ErrUnsafeShortcut
		}
		seen[p] = true
		switch p {
		case "fn", "cmd", "ctrl", "alt", "leftoption", "rightoption", "leftcommand", "rightcommand", "leftcontrol", "rightcontrol":
			strong = true
		case "shift", "leftshift", "rightshift":
		default:
			if ordinary != "" || i != len(parts)-1 || !shortcutKey(p) {
				return ErrUnsafeShortcut
			}
			ordinary = p
		}
	}
	if ordinary == "" || strong || functionKey(ordinary) {
		return nil
	}
	return ErrUnsafeShortcut
}

func functionKey(key string) bool {
	if !strings.HasPrefix(key, "f") {
		return false
	}
	n, err := strconv.Atoi(key[1:])
	return err == nil && n >= 1 && n <= 20 && strconv.Itoa(n) == key[1:]
}

func shortcutKey(key string) bool {
	if len(key) == 1 && strings.Contains("abcdefghijklmnopqrstuvwxyz0123456789=[];'/.,`-\\", key) {
		return true
	}
	if functionKey(key) {
		return true
	}
	if len(key) == 4 && strings.HasPrefix(key, "num") && key[3] >= '0' && key[3] <= '9' {
		return true
	}
	switch key {
	case "plus", "enter", "return", "space", "tab", "escape", "esc", "backspace", "delete", "del", "insert", "ins", "home", "end", "pageup", "pagedown", "left", "right", "up", "down", "numdec", "nummult", "numadd", "numdiv", "numsub", "volumeup", "volumedown", "volumemute":
		return true
	}
	return false
}
