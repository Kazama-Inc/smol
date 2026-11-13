//go:build !unix && !windows

package smol

// Local machines run only on unix hosts, so there is nothing to clean up.
func installSignalCleanup() {}

func armProcessReaper() {}

func detachSupported() bool { return true }
