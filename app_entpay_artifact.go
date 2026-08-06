package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type EntPayArtifactPreview struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func (a *App) GetEntPayArtifactPreview(id string) (EntPayArtifactPreview, error) {
	path, err := a.verifiedEntPayArtifact(id)
	if err != nil {
		return EntPayArtifactPreview{}, err
	}
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return EntPayArtifactPreview{}, err
	}
	detail, err := manager.SessionDetail(ctx, id)
	if err != nil {
		return EntPayArtifactPreview{}, err
	}
	mediaType := strings.ToLower(strings.TrimSpace(detail.Session.ArtifactMediaType))
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		return EntPayArtifactPreview{}, fmt.Errorf("artifact type is not previewable")
	}
	contents, err := os.ReadFile(path)
	if err != nil || int64(len(contents)) != detail.Session.ArtifactBytes {
		return EntPayArtifactPreview{}, fmt.Errorf("artifact changed while opening preview")
	}
	return EntPayArtifactPreview{MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(contents)}, nil
}

func (a *App) OpenEntPayArtifact(id string) (ActionResult, error) {
	path, err := a.verifiedEntPayArtifact(id)
	if err != nil {
		return ActionResult{}, err
	}
	if err := openEntPayArtifact(path, false); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{ID: id, Message: "Verified artifact opened"}, nil
}

func (a *App) RevealEntPayArtifact(id string) (ActionResult, error) {
	path, err := a.verifiedEntPayArtifact(id)
	if err != nil {
		return ActionResult{}, err
	}
	if err := openEntPayArtifact(path, true); err != nil {
		return ActionResult{}, err
	}
	return ActionResult{ID: id, Message: "Verified artifact revealed"}, nil
}

func (a *App) verifiedEntPayArtifact(id string) (string, error) {
	manager, ctx, err := a.requireEntPayClient()
	if err != nil {
		return "", err
	}
	detail, err := manager.SessionDetail(ctx, id)
	if err != nil {
		return "", err
	}
	session := detail.Session
	if session.Stage != "complete" || session.ArtifactPath == "" || session.ArtifactSHA256 == "" || session.ArtifactBytes < 0 {
		return "", fmt.Errorf("EntPay session has no verified artifact")
	}
	path, err := filepath.Abs(session.ArtifactPath)
	if err != nil {
		return "", fmt.Errorf("artifact path is invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != session.ArtifactBytes {
		return "", fmt.Errorf("artifact changed after delivery")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(path)) {
		return "", fmt.Errorf("artifact path is no longer trusted")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, io.LimitReader(file, session.ArtifactBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), session.ArtifactSHA256) {
		return "", fmt.Errorf("artifact failed integrity verification")
	}
	return path, nil
}
