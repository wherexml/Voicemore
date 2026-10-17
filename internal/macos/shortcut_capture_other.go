//go:build !darwin

package macos

func CaptureShortcut(uintptr, func(string), func(string)) (func(), error) { return nil, errUnsupported }
func StartTranslationHotkey(string, func(bool)) (func(), error)           { return nil, errUnsupported }
