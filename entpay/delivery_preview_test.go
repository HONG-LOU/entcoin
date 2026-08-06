package entpay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClientArtifactNamesAndConflictResolution(t *testing.T) {
	for _, name := range []string{"result.jpg", "报告 2026.json", "archive.tar.gz"} {
		if !validClientArtifactName(name) {
			t.Fatalf("valid artifact name rejected: %q", name)
		}
	}
	for _, name := range []string{"", "../result.jpg", "folder/result.jpg", `folder\result.jpg`, "CON", "nul.txt", "result.svg\n.exe", "result.", "result?.jpg"} {
		if validClientArtifactName(name) {
			t.Fatalf("unsafe artifact name accepted: %q", name)
		}
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "result.jpg"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := availableArtifactPath(directory, "result.jpg", "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "result-01234567.jpg" {
		t.Fatalf("conflict path = %s", path)
	}
}

func TestSecureArtifactDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := secureArtifactDirectory(link); err == nil {
		t.Fatal("symlink artifact directory was accepted")
	}
}
