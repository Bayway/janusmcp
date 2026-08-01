//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func detachDaemonCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
}
