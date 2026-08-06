//go:build !windows && !linux

package main

import "fmt"

func getEntPayLinkStatus() (EntPayLinkStatus, error) {
	return EntPayLinkStatus{Message: "EntPay link registration is unsupported on this platform"}, nil
}

func registerEntPayLinks() (EntPayLinkStatus, error) {
	return EntPayLinkStatus{}, fmt.Errorf("EntPay link registration is unsupported on this platform")
}
