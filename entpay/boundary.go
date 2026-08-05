package entpay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxRequestBytes = 128 << 10

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

func canonicalObject(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || len(raw) > maxRequestBytes {
		return nil, fmt.Errorf("input must be a bounded JSON object")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("input is invalid JSON: %w", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("input contains trailing data")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonicalize input: %w", err)
	}
	return canonical, nil
}

func canonicalPayload(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("fulfillment payload must be bounded JSON")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("fulfillment payload is invalid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("fulfillment payload contains trailing data")
	}
	return json.Marshal(value)
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

func validateProductDescriptor(descriptor ProductDescriptor) error {
	if !validIdentifier(descriptor.ID) || len(strings.TrimSpace(descriptor.Name)) < 3 || len(descriptor.Name) > 80 || len(strings.TrimSpace(descriptor.Description)) < 10 || len(descriptor.Description) > 240 {
		return fmt.Errorf("product identity is invalid")
	}
	if descriptor.Price == 0 || descriptor.Confirmations == 0 || descriptor.Confirmations > 100 {
		return fmt.Errorf("product payment terms are invalid")
	}
	if descriptor.Accent != "green" && descriptor.Accent != "coral" && descriptor.Accent != "blue" && descriptor.Accent != "gold" {
		return fmt.Errorf("product accent is invalid")
	}
	if len(descriptor.Fields) == 0 || len(descriptor.Fields) > 12 {
		return fmt.Errorf("product fields are invalid")
	}
	seen := make(map[string]struct{})
	for _, field := range descriptor.Fields {
		if !validIdentifier(field.ID) || len(strings.TrimSpace(field.Label)) < 2 || len(field.Label) > 80 || (field.Type != "text" && field.Type != "textarea") || field.MinLength < 0 || field.MaxLength < field.MinLength || field.MaxLength > 10_000 {
			return fmt.Errorf("product input field is invalid")
		}
		if _, exists := seen[field.ID]; exists {
			return fmt.Errorf("product input field is duplicated")
		}
		seen[field.ID] = struct{}{}
	}
	return nil
}

func validIdentifier(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9' && index > 0) || (character == '-' && index > 0 && index < len(value)-1) {
			continue
		}
		return false
	}
	return true
}

func validateFulfillment(value Fulfillment) ([]byte, error) {
	payload, err := canonicalPayload(value.Payload)
	if err != nil {
		return nil, err
	}
	if value.Artifact == nil {
		return payload, nil
	}
	if len(value.Artifact.Contents) == 0 || len(value.Artifact.Contents) > maxArtifactBytes || !validMediaType(value.Artifact.MediaType) {
		return nil, fmt.Errorf("fulfillment artifact is invalid")
	}
	name := filepath.Base(strings.TrimSpace(value.Artifact.FileName))
	if name == "." || name == "" || name != value.Artifact.FileName || len(name) > 120 {
		return nil, fmt.Errorf("artifact file name is invalid")
	}
	return payload, nil
}

func validMediaType(value string) bool {
	if len(value) == 0 || len(value) > 80 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	return err == nil && len(parameters) == 0 && mediaType == strings.ToLower(value) && strings.Contains(mediaType, "/")
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
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
