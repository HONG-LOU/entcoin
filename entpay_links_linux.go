//go:build linux

package main

import (
	"errors"
	"os/exec"
	"strings"
)

func getEntPayLinkStatus() (EntPayLinkStatus, error) {
	output, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/entcoin").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return EntPayLinkStatus{Message: "Install the Entcoin desktop package to register EntPay links"}, nil
		}
		return EntPayLinkStatus{Message: "EntPay link registration is not active"}, nil
	}
	owner := strings.TrimSpace(string(output))
	return EntPayLinkStatus{Registered: owner == "entcoin.desktop", Owner: owner, Message: "EntPay link registration inspected through the desktop MIME database"}, nil
}

func registerEntPayLinks() (EntPayLinkStatus, error) {
	if err := exec.Command("xdg-mime", "default", "entcoin.desktop", "x-scheme-handler/entcoin").Run(); err != nil {
		return EntPayLinkStatus{}, err
	}
	return getEntPayLinkStatus()
}
