// +build darwin dragonfly freebsd linux netbsd openbsd solaris

package main

import (
	"os/exec"
	"syscall"
)

func sendSignalContinue(job *exec.Cmd) error {
	return job.Process.Signal(syscall.SIGCONT)
}
