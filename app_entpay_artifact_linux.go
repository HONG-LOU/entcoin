//go:build linux

package main

import (
	"os/exec"
	"path/filepath"
)

func openEntPayArtifact(path string, reveal bool) error {
	target := path
	if reveal {
		target = filepath.Dir(path)
	}
	return exec.Command("xdg-open", target).Start()
}
