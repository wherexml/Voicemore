//go:build !darwin

package macos

func AppIconPNG(bundleID, name string) []byte { return nil }
