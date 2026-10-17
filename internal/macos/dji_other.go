//go:build !darwin

package macos

func StartDJIButton(func()) (func(), error) { return func() {}, nil }

func StartDJIButtonMonitor(bool, func()) (func(), error) { return nil, errUnsupported }
func DJIConnected() bool                                 { return false }
