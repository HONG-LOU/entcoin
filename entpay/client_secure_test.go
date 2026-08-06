package entpay

import (
	"bytes"
	"errors"
	"testing"
)

func TestClientProtectorBindsSessionAndSchema(t *testing.T) {
	protector, err := newXChaChaProtector(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "0123456789abcdef0123456789abcdef"
	plaintext := []byte(`{"claim_token":"secret","input":{"prompt":"private"}}`)
	ciphertext, err := protector.Seal(sessionID, clientCapsuleSchema, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("secret")) || bytes.Contains(ciphertext, []byte("private")) {
		t.Fatal("session capsule contains plaintext")
	}
	opened, err := protector.Open(sessionID, clientCapsuleSchema, ciphertext)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open capsule = %q, %v", opened, err)
	}
	for name, mutate := range map[string]func() (string, uint32, []byte){
		"session": func() (string, uint32, []byte) {
			return "1123456789abcdef0123456789abcdef", clientCapsuleSchema, ciphertext
		},
		"schema": func() (string, uint32, []byte) { return sessionID, clientCapsuleSchema + 1, ciphertext },
		"tamper": func() (string, uint32, []byte) {
			changed := append([]byte(nil), ciphertext...)
			changed[len(changed)-1] ^= 1
			return sessionID, clientCapsuleSchema, changed
		},
	} {
		t.Run(name, func(t *testing.T) {
			id, schema, value := mutate()
			if _, err := protector.Open(id, schema, value); err == nil || (name != "schema" && !errors.Is(err, ErrClientSecretAuthentication)) {
				t.Fatalf("authentication error = %v", err)
			}
		})
	}
}
