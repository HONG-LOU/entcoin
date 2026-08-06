package entpay

import (
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/crypto/chacha20poly1305"
)

func OpenDesktopClientStore(dataDirectory string) (*ClientStore, error) {
	protector, err := newPlatformClientProtector()
	if err != nil {
		return nil, err
	}
	return OpenClientStore(filepath.Join(dataDirectory, "entpay-client.db"), protector)
}

const clientCapsuleSchema = uint32(1)

var ErrClientSecretAuthentication = errors.New("EntPay session secret authentication failed")

type clientProtector interface {
	Seal(sessionID string, schema uint32, plaintext []byte) ([]byte, error)
	Open(sessionID string, schema uint32, ciphertext []byte) ([]byte, error)
}

type xChaChaProtector struct {
	key [chacha20poly1305.KeySize]byte
}

func newXChaChaProtector(key []byte) (*xChaChaProtector, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("EntPay session key has an invalid size")
	}
	protector := &xChaChaProtector{}
	copy(protector.key[:], key)
	return protector, nil
}

func (p *xChaChaProtector) Seal(sessionID string, schema uint32, plaintext []byte) ([]byte, error) {
	if err := validateClientSecretArguments(sessionID, schema, plaintext); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(p.key[:])
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("create EntPay session nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, clientSecretAAD(sessionID, schema)), nil
}

func (p *xChaChaProtector) Open(sessionID string, schema uint32, ciphertext []byte) ([]byte, error) {
	if err := validateClientSecretArguments(sessionID, schema, ciphertext); err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(p.key[:])
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrClientSecretAuthentication
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], clientSecretAAD(sessionID, schema))
	if err != nil {
		return nil, ErrClientSecretAuthentication
	}
	return plaintext, nil
}

func validateClientSecretArguments(sessionID string, schema uint32, value []byte) error {
	if validateSessionID(sessionID) != nil || schema != clientCapsuleSchema || len(value) == 0 || len(value) > 256<<10 {
		return fmt.Errorf("EntPay session secret arguments are invalid")
	}
	return nil
}

func clientSecretAAD(sessionID string, schema uint32) []byte {
	return []byte(fmt.Sprintf("entropy-mainnet-v1\x00entpay-client\x00%d\x00%s", schema, sessionID))
}

func validateSessionID(value string) error {
	if len(value) != 32 {
		return fmt.Errorf("EntPay session ID is invalid")
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("EntPay session ID is invalid")
		}
	}
	return nil
}
