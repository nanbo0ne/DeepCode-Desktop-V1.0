//go:build !windows

package computeruse

func newNativeBackend() Backend { return nil }
