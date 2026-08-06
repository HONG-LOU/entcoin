//go:build linux

package entpay

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	clientSecretService = "Entcoin"
	clientSecretAccount = "mainnet-v1-entpay-session-key"
)

func newPlatformClientProtector() (clientProtector, error) {
	encoded, err := keyring.Get(clientSecretService, clientSecretAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		key := make([]byte, chacha20poly1305.KeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate EntPay session key: %w", err)
		}
		encoded = base64.StdEncoding.EncodeToString(key)
		clear(key)
		if err := keyring.Set(clientSecretService, clientSecretAccount, encoded); err != nil {
			return nil, fmt.Errorf("store EntPay session key in Secret Service: %w", err)
		}
		verified, err := keyring.Get(clientSecretService, clientSecretAccount)
		if err != nil || verified != encoded {
			return nil, fmt.Errorf("verify EntPay session key in Secret Service")
		}
	} else if err != nil {
		return nil, fmt.Errorf("open EntPay session key from Secret Service: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != chacha20poly1305.KeySize {
		clear(key)
		return nil, fmt.Errorf("EntPay session key in Secret Service is invalid")
	}
	defer clear(key)
	return newXChaChaProtector(key)
}
