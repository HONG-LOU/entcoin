package entpay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const (
	maxLaunchURLBytes = 2048
	handoffTokenBytes = 32
)

type LaunchRequest struct {
	Merchant string
	Handoff  string
}

func ParseLaunchURL(value string) (LaunchRequest, error) {
	if len(value) == 0 || len(value) > maxLaunchURLBytes {
		return LaunchRequest{}, fmt.Errorf("payment link length is invalid")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "entcoin" || parsed.Host != "pay" || parsed.Path != "" || parsed.User != nil || parsed.Fragment != "" {
		return LaunchRequest{}, fmt.Errorf("payment link is invalid")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || (len(query) != 2 && len(query) != 3) {
		return LaunchRequest{}, fmt.Errorf("payment link parameters are invalid")
	}
	for _, name := range []string{"v", "merchant"} {
		if len(query[name]) != 1 {
			return LaunchRequest{}, fmt.Errorf("payment link parameters are invalid")
		}
	}
	if query.Get("v") != "1" {
		return LaunchRequest{}, fmt.Errorf("payment link version is unsupported")
	}
	merchant, err := validatePublicEndpoint(query.Get("merchant"))
	if err != nil || merchant != query.Get("merchant") {
		return LaunchRequest{}, fmt.Errorf("payment link merchant is invalid")
	}
	handoff := ""
	if values, present := query["handoff"]; present {
		if len(values) != 1 || validateOpaqueToken(values[0]) != nil {
			return LaunchRequest{}, fmt.Errorf("payment link handoff is invalid")
		}
		handoff = values[0]
	}
	return LaunchRequest{Merchant: merchant, Handoff: handoff}, nil
}

func buildLaunchURL(endpoint string) string {
	query := url.Values{"v": {"1"}, "merchant": {endpoint}}
	return (&url.URL{Scheme: "entcoin", Host: "pay", RawQuery: query.Encode()}).String()
}

func buildHandoffURL(endpoint, code string) string {
	query := url.Values{"v": {"1"}, "merchant": {endpoint}, "handoff": {code}}
	return (&url.URL{Scheme: "entcoin", Host: "pay", RawQuery: query.Encode()}).String()
}

func ValidateLaunchRequest(request LaunchRequest) error {
	merchant, err := validatePublicEndpoint(request.Merchant)
	if err != nil || merchant != request.Merchant || validateOpaqueToken(request.Handoff) != nil {
		return fmt.Errorf("EntPay launch request is invalid")
	}
	return nil
}

func ValidateLaunchIntent(request LaunchRequest) error {
	merchant, err := validatePublicEndpoint(request.Merchant)
	if err != nil || merchant != request.Merchant || request.Handoff != "" {
		return fmt.Errorf("EntPay launch intent is invalid")
	}
	return nil
}

func newOpaqueToken() (string, error) {
	value := make([]byte, handoffTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func validateOpaqueToken(value string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) < handoffTokenBytes || len(decoded) > 64 || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return fmt.Errorf("token must be unpadded base64url with at least 256 bits")
	}
	return nil
}

func opaqueTokenDigest(value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(value))
}

func validatePublicEndpoint(value string) (string, error) {
	endpoint, err := normalizeEntPayURL(value)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(endpoint)
	port := parsed.Port()
	if port != "" && port != "443" && port != "80" {
		host := parsed.Hostname()
		address, addressErr := netip.ParseAddr(host)
		if host != "localhost" && (addressErr != nil || !address.IsLoopback()) {
			return "", fmt.Errorf("EntPay endpoint uses a non-default public port")
		}
	}
	if strings.ContainsAny(endpoint, "\r\n\x00") {
		return "", fmt.Errorf("EntPay endpoint is invalid")
	}
	if host, _, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil && strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("EntPay endpoint is invalid")
	}
	return endpoint, nil
}
