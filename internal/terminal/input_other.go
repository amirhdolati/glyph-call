//go:build !darwin && !linux

package terminal

func suspendInputEcho() func() { return func() {} }
