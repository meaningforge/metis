//go:build !darwin && !linux

package lifecycle

import (
	"fmt"
	"syscall"
)

func detachedProcessAttributes() *syscall.SysProcAttr { return nil }
func processGroupID(int) (int, error) {
	return 0, fmt.Errorf("S2SBench lifecycle is supported only on Linux and macOS")
}
func processExists(int) bool      { return false }
func processGroupExists(int) bool { return false }
func signalProcessGroup(int, bool) error {
	return fmt.Errorf("S2SBench lifecycle is supported only on Linux and macOS")
}
func processStartIdentity(int) (string, error) {
	return "", fmt.Errorf("S2SBench lifecycle is supported only on Linux and macOS")
}
func executableIdentity(int) (string, error) {
	return "", fmt.Errorf("S2SBench lifecycle is supported only on Linux and macOS")
}
