package entpay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

func decodeJSON(request *http.Request, target any) error {
	contents, err := io.ReadAll(io.LimitReader(request.Body, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}
	if len(contents) > maxRequestBytes {
		return fmt.Errorf("JSON exceeds %d bytes", maxRequestBytes)
	}
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON contains trailing data")
	}
	return nil
}

func rejectDuplicateJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("JSON nesting exceeds 32 levels")
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("scan JSON: %w", err)
		}
		delimiter, nested := token.(json.Delim)
		if !nested {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("scan JSON key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is invalid")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("JSON contains duplicate key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("JSON delimiter is invalid")
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("scan JSON closing delimiter: %w", err)
		}
		return nil
	}
	return walk(0)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func (s *Service) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://entcoin.xyz; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}

func (s *Service) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		s.logger.Info("request", "method", request.Method, "path", request.URL.Path, "client_ip", clientIP(request), "duration_ms", time.Since(started).Milliseconds())
	})
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	if remote.IsLoopback() {
		forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0])
		if candidate, err := netip.ParseAddr(forwarded); err == nil {
			return candidate.Unmap().String()
		}
	}
	return remote.Unmap().String()
}

type rateState struct {
	started time.Time
	count   int
}

type rateLimiter struct {
	mu     sync.Mutex
	states map[string]rateState
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{states: make(map[string]rateState)}
}

func (l *rateLimiter) Allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	state := l.states[key]
	if state.started.IsZero() || now.Sub(state.started) >= window {
		l.states[key] = rateState{started: now, count: 1}
		return true
	}
	if state.count >= limit {
		return false
	}
	state.count++
	l.states[key] = state
	if len(l.states) > 4096 {
		for candidate, value := range l.states {
			if now.Sub(value.started) >= window {
				delete(l.states, candidate)
			}
		}
	}
	return true
}
