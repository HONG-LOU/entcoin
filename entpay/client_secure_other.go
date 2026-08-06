//go:build !windows && !linux

package entpay

import "fmt"

func newPlatformClientProtector() (clientProtector, error) {
	return nil, fmt.Errorf("Agent Pay secret protection is unavailable on this platform")
}
