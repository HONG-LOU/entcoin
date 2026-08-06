//go:build !windows && !linux

package main

import "fmt"

func openEntPayArtifact(string, bool) error {
	return fmt.Errorf("opening EntPay artifacts is unsupported on this platform")
}
