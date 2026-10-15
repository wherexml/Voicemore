//go:build !darwin

package macos

func StartDJIButton(func()) (func(), error) { return func() {}, nil }
