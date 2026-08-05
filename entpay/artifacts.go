package entpay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type fulfillmentStore struct {
	directory string
}

type stagedFulfillment struct {
	Payload  json.RawMessage   `json:"payload"`
	Artifact *ArtifactMetadata `json:"artifact,omitempty"`
}

func openFulfillmentStore(directory string) (*fulfillmentStore, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("fulfillment directory is required")
	}
	for _, child := range []string{"artifacts", "staging"} {
		if err := os.MkdirAll(filepath.Join(directory, child), 0o700); err != nil {
			return nil, fmt.Errorf("create fulfillment directory: %w", err)
		}
	}
	return &fulfillmentStore{directory: directory}, nil
}

func (s *fulfillmentStore) Stage(invoiceID string, fulfillment Fulfillment, payload []byte) (stagedFulfillment, error) {
	if err := validateInvoiceID(invoiceID); err != nil {
		return stagedFulfillment{}, err
	}
	staged := stagedFulfillment{Payload: append(json.RawMessage(nil), payload...)}
	if fulfillment.Artifact != nil {
		artifactPath := s.artifactPath(invoiceID)
		if err := writeAtomic(artifactPath, fulfillment.Artifact.Contents, 0o600); err != nil {
			return stagedFulfillment{}, fmt.Errorf("store fulfillment artifact: %w", err)
		}
		staged.Artifact = &ArtifactMetadata{
			DownloadPath: "v1/invoices/" + invoiceID + "/artifact",
			SHA256:       contentHash(fulfillment.Artifact.Contents), MediaType: fulfillment.Artifact.MediaType,
			FileName: fulfillment.Artifact.FileName, Bytes: int64(len(fulfillment.Artifact.Contents)),
		}
	}
	encoded, err := json.Marshal(staged)
	if err != nil {
		return stagedFulfillment{}, fmt.Errorf("encode staged fulfillment: %w", err)
	}
	if err := writeAtomic(s.stagingPath(invoiceID), encoded, 0o600); err != nil {
		return stagedFulfillment{}, fmt.Errorf("store staged fulfillment: %w", err)
	}
	return staged, nil
}

func (s *fulfillmentStore) Existing(invoiceID string) (stagedFulfillment, bool, error) {
	if err := validateInvoiceID(invoiceID); err != nil {
		return stagedFulfillment{}, false, err
	}
	contents, err := os.ReadFile(s.stagingPath(invoiceID))
	if errors.Is(err, os.ErrNotExist) {
		return stagedFulfillment{}, false, nil
	}
	if err != nil {
		return stagedFulfillment{}, false, err
	}
	if len(contents) > 2<<20 {
		return stagedFulfillment{}, false, fmt.Errorf("staged fulfillment is too large")
	}
	var staged stagedFulfillment
	if err := json.Unmarshal(contents, &staged); err != nil {
		return stagedFulfillment{}, false, fmt.Errorf("staged fulfillment is invalid")
	}
	payload, err := canonicalPayload(staged.Payload)
	if err != nil || string(payload) != string(staged.Payload) {
		return stagedFulfillment{}, false, fmt.Errorf("staged payload is invalid")
	}
	if staged.Artifact != nil {
		artifact, err := s.ReadArtifact(invoiceID)
		if err != nil || int64(len(artifact)) != staged.Artifact.Bytes || contentHash(artifact) != staged.Artifact.SHA256 {
			return stagedFulfillment{}, false, fmt.Errorf("staged artifact is invalid")
		}
	}
	return staged, true, nil
}

func (s *fulfillmentStore) ReadArtifact(invoiceID string) ([]byte, error) {
	if err := validateInvoiceID(invoiceID); err != nil {
		return nil, err
	}
	file, err := os.Open(s.artifactPath(invoiceID))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxArtifactBytes {
		return nil, fmt.Errorf("artifact is too large")
	}
	return contents, nil
}

func (s *fulfillmentStore) RemoveStage(invoiceID string) {
	_ = os.Remove(s.stagingPath(invoiceID))
}

func (s *fulfillmentStore) artifactPath(invoiceID string) string {
	return filepath.Join(s.directory, "artifacts", invoiceID+".bin")
}

func (s *fulfillmentStore) stagingPath(invoiceID string) string {
	return filepath.Join(s.directory, "staging", invoiceID+".json")
}

func validateInvoiceID(invoiceID string) error {
	if len(invoiceID) != 32 {
		return fmt.Errorf("invoice ID is invalid")
	}
	for _, character := range invoiceID {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("invoice ID is invalid")
		}
	}
	return nil
}

func writeAtomic(path string, contents []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".entpay-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
