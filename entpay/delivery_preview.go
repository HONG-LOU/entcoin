package entpay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func downloadClientArtifact(ctx context.Context, client *http.Client, endpoint, invoiceID, directory, sessionID string, delivery Delivery, token func() (string, error)) (string, error) {
	metadata := delivery.Artifact
	if metadata == nil || metadata.DownloadPath != "v1/invoices/"+invoiceID+"/artifact" || !validClientArtifactName(metadata.FileName) {
		return "", fmt.Errorf("artifact metadata is invalid")
	}
	root, err := secureArtifactDirectory(directory)
	if err != nil {
		return "", err
	}
	destination, err := availableArtifactPath(root, metadata.FileName, invoiceID)
	if err != nil {
		return "", err
	}
	claimToken, err := token()
	if err != nil || claimToken == "" {
		return "", fmt.Errorf("artifact capability is unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+metadata.DownloadPath, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+claimToken)
	request.Header.Set("Accept", metadata.MediaType)
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("artifact download returned HTTP %d", response.StatusCode)
	}
	contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	if contentType != metadata.MediaType {
		return "", fmt.Errorf("artifact media type does not match Receipt")
	}
	temporary, err := os.CreateTemp(root, ".entpay-"+sessionID[:8]+"-*.part")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(response.Body, maxArtifactBytes+1))
	if copyErr != nil || written != metadata.Bytes || written > maxArtifactBytes || hex.EncodeToString(digest.Sum(nil)) != metadata.SHA256 {
		temporary.Close()
		return "", fmt.Errorf("artifact size or SHA-256 does not match Receipt")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		return "", fmt.Errorf("save artifact without overwrite: %w", err)
	}
	if directoryHandle, err := os.Open(root); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return destination, nil
}

func secureArtifactDirectory(directory string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(strings.TrimSpace(directory)))
	if err != nil || strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("artifact directory is invalid")
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("artifact directory is unsafe")
	}
	for component := absolute; ; component = filepath.Dir(component) {
		unsafe, err := unsafeArtifactPathComponent(component)
		if err != nil || unsafe {
			return "", fmt.Errorf("artifact directory must not contain symbolic links or reparse points")
		}
		if parent := filepath.Dir(component); parent == component {
			break
		}
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return "", err
	}
	return absolute, nil
}

func validClientArtifactName(value string) bool {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > 120 || filepath.Base(name) != name || strings.HasSuffix(name, ".") || strings.ContainsAny(name, "<>:\"/\\|?*\x00\r\n") {
		return false
	}
	for _, character := range name {
		if character < 0x20 {
			return false
		}
	}
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	reserved := map[string]struct{}{"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}}
	for index := 1; index <= 9; index++ {
		reserved[fmt.Sprintf("COM%d", index)] = struct{}{}
		reserved[fmt.Sprintf("LPT%d", index)] = struct{}{}
	}
	_, blocked := reserved[base]
	return !blocked
}

func availableArtifactPath(directory, suggested, invoiceID string) (string, error) {
	base := strings.TrimSuffix(suggested, filepath.Ext(suggested))
	extension := filepath.Ext(suggested)
	candidates := []string{suggested, base + "-" + invoiceID[:8] + extension}
	for index := 2; index <= 100; index++ {
		candidates = append(candidates, fmt.Sprintf("%s-%s-%d%s", base, invoiceID[:8], index, extension))
	}
	for _, name := range candidates {
		path := filepath.Join(directory, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("artifact destination has too many conflicts")
}
