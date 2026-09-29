//go:build !windows

package main

// totalMemory is a non-Windows stub. On Linux/darwin you can extend this with
// syscall.Sysinfo or gopsutil if needed; for now it reports 0.
func totalMemory() (uint64, error) {
	return 0, nil
}
