//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const entPayProtocolKey = `Software\Classes\entcoin`

func expectedEntPayCommand() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`"%s" "%%1"`, executable), nil
}

func getEntPayLinkStatus() (EntPayLinkStatus, error) {
	expected, err := expectedEntPayCommand()
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, entPayProtocolKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return EntPayLinkStatus{Portable: true, Message: "EntPay links are not registered for this Windows user"}, nil
		}
		return EntPayLinkStatus{}, err
	}
	defer key.Close()
	command, _, err := key.GetStringValue("")
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	return EntPayLinkStatus{Registered: strings.EqualFold(command, expected), Owner: command, Portable: true, Message: "EntPay link registration inspected for this Windows user"}, nil
}

func registerEntPayLinks() (EntPayLinkStatus, error) {
	command, err := expectedEntPayCommand()
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	root, _, err := registry.CreateKey(registry.CURRENT_USER, entPayProtocolKey, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	if err := root.SetStringValue("", "Entcoin Agent Pay link"); err != nil {
		root.Close()
		return EntPayLinkStatus{}, err
	}
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		root.Close()
		return EntPayLinkStatus{}, err
	}
	root.Close()
	icon, _, err := registry.CreateKey(registry.CURRENT_USER, entPayProtocolKey+`\DefaultIcon`, registry.SET_VALUE)
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	_ = icon.SetStringValue("", strings.TrimSuffix(command, ` "%1"`)+",0")
	icon.Close()
	open, _, err := registry.CreateKey(registry.CURRENT_USER, entPayProtocolKey+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return EntPayLinkStatus{}, err
	}
	if err := open.SetStringValue("", command); err != nil {
		open.Close()
		return EntPayLinkStatus{}, err
	}
	open.Close()
	return getEntPayLinkStatus()
}
