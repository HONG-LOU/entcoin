//go:build windows

package entpay

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dpapiClientProtector struct{}

func newPlatformClientProtector() (clientProtector, error) {
	return dpapiClientProtector{}, nil
}

func (dpapiClientProtector) Seal(sessionID string, schema uint32, plaintext []byte) ([]byte, error) {
	if err := validateClientSecretArguments(sessionID, schema, plaintext); err != nil {
		return nil, err
	}
	return clientCryptProtect(plaintext, clientSecretAAD(sessionID, schema), true)
}

func (dpapiClientProtector) Open(sessionID string, schema uint32, ciphertext []byte) ([]byte, error) {
	if err := validateClientSecretArguments(sessionID, schema, ciphertext); err != nil {
		return nil, err
	}
	plaintext, err := clientCryptProtect(ciphertext, clientSecretAAD(sessionID, schema), false)
	if errors.Is(err, windows.ERROR_INVALID_DATA) {
		return nil, ErrClientSecretAuthentication
	}
	return plaintext, err
}

func clientCryptProtect(input, entropy []byte, protect bool) ([]byte, error) {
	inputBlob := clientDataBlob(input)
	entropyBlob := clientDataBlob(entropy)
	var output windows.DataBlob
	var err error
	if protect {
		description, conversionErr := windows.UTF16PtrFromString("Entcoin Agent Pay session")
		if conversionErr != nil {
			return nil, conversionErr
		}
		err = windows.CryptProtectData(&inputBlob, description, &entropyBlob, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
		runtime.KeepAlive(description)
	} else {
		err = windows.CryptUnprotectData(&inputBlob, nil, &entropyBlob, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(input)
	runtime.KeepAlive(entropy)
	if err != nil {
		return nil, errors.Join(err, releaseClientDPAPIOutput(&output))
	}
	if output.Data == nil || output.Size == 0 || output.Size > 300<<10 {
		return nil, errors.Join(fmt.Errorf("DPAPI returned an invalid EntPay session secret"), releaseClientDPAPIOutput(&output))
	}
	result := append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...)
	if err := releaseClientDPAPIOutput(&output); err != nil {
		clear(result)
		return nil, err
	}
	return result, nil
}

func releaseClientDPAPIOutput(output *windows.DataBlob) error {
	if output == nil || output.Data == nil {
		return nil
	}
	if output.Size > 0 {
		clear(unsafe.Slice(output.Data, int(output.Size)))
	}
	_, err := windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	output.Data = nil
	output.Size = 0
	return err
}

func clientDataBlob(value []byte) windows.DataBlob {
	if len(value) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
}
