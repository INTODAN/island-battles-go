//go:build !unix

package main

import "os/exec"

// ownProcessGroup does nothing where process groups aren't available.
func ownProcessGroup(*exec.Cmd) {}

// killWithChildren kills command's process. Processes it started are not
// reached here, so a worker command should be the bot's own process.
func killWithChildren(command *exec.Cmd) {
	_ = command.Process.Kill()
}
