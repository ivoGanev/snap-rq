//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// totalMemory returns the total amount of physical memory installed on the
// machine by querying Win32_ComputerSystem via PowerShell.
func totalMemory() (uint64, error) {
	out, err := exec.Command(
		"powershell",
		"-NoProfile",
		"-Command",
		"(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory",
	).Output()
	if err != nil {
		return 0, nil
	}

	val, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, nil
	}
	return val, nil
}
