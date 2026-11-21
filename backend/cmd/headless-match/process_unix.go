//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts command in a process group of its own, so that
// killWithChildren reaches the processes it starts and a signal to this
// command's group, such as Ctrl+C in a terminal, doesn't reach the workers
// before the command has stopped them itself.
func ownProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killWithChildren kills command's process group: the process and whatever
// it started that didn't leave the group.
func killWithChildren(command *exec.Cmd) {
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
		_ = command.Process.Kill()
	}
}
