//go:build !darwin && !linux

package command

import (
	"fmt"
	"os/exec"
)

func configureAgentProcess(*exec.Cmd) error {
	return fmt.Errorf("S2SBench Agent process control is supported only on Linux and macOS")
}

func terminateAgentProcess(int, bool) error {
	return fmt.Errorf("S2SBench Agent process control is supported only on Linux and macOS")
}
