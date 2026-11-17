//go:build windows

package main

import "os/exec"

func rbOwnGroup(c *exec.Cmd)  {}
func rbKillGroup(c *exec.Cmd) {}
func rbCancel(c *exec.Cmd)    {}
