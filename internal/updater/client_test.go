package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testReleaseVersion = "1.5.2"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.7", right: "1.0.7"},
		{left: "1.0.8", right: "1.0.7", want: 1},
		{left: "1.1.0", right: "1.9.9", want: -1},
	}
	for _, test := range tests {
		got, err := compareVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, %v; want %d", test.left, test.right, got, err, test.want)
		}
	}
	if _, err := compareVersions("1.0", "1.0.7"); err == nil {
		t.Fatal("non-canonical version was accepted")
	}
}

func TestLatestStableEntryIgnoresPrereleasesAndSelectsHighestVersion(t *testing.T) {
	entries := []atomEntry{
		{Title: "v1.0.7"},
		{Title: "v1.5.2-rc1"},
		{Title: "v1.0.9"},
		{Title: "v1.0.10"},
		{Title: "v1.0.15"},
		{Title: "v1.0.16"},
		{Title: "v1.5.2"},
	}

	entry, version, err := latestStableEntry(entries)
	if err != nil {
		t.Fatal(err)
	}
	if version != testReleaseVersion || entry.Title != "v"+testReleaseVersion {
		t.Fatalf("latest stable entry = %q (%q), want v%s", entry.Title, version, testReleaseVersion)
	}
}

func TestCheckSelectsLinuxUpdate(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, _ := testClient(t, artifact, false)
	defer server.Close()

	status, err := client.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.CurrentVersion != CurrentVersion || status.LatestVersion != testReleaseVersion {
		t.Fatalf("unexpected update status: %#v", status)
	}
	if status.AssetName != "entcoin_"+testReleaseVersion+"_amd64.deb" {
		t.Fatalf("asset = %q", status.AssetName)
	}
}

func TestWindowsUpdateSelectsPortableExecutable(t *testing.T) {
	artifact, checksum, err := assetNames("windows", "amd64", testReleaseVersion)
	if err != nil {
		t.Fatal(err)
	}
	if artifact != "Entcoin.exe" || checksum != "SHA256SUMS.txt" {
		t.Fatalf("Windows update assets = %q, %q", artifact, checksum)
	}
}

func TestPrepareLatestVerifiesAndCachesArtifact(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, _ := testClient(t, artifact, false)
	defer server.Close()
	var progress []Progress

	prepared, err := client.PrepareLatest(context.Background(), func(update Progress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(artifact) {
		t.Fatalf("downloaded artifact = %q", data)
	}
	if filepath.Dir(prepared.Path) != client.cacheRoot {
		t.Fatalf("update escaped cache root: %s", prepared.Path)
	}
	if len(progress) < 4 || progress[0].Phase != "preparing" || progress[len(progress)-1].Phase != "verifying" {
		t.Fatalf("progress events = %#v", progress)
	}
	download := progress[len(progress)-2]
	if download.Phase != "downloading" || download.Downloaded != int64(len(artifact)) || download.Total != int64(len(artifact)) || download.Percent != 100 {
		t.Fatalf("final download progress = %#v", download)
	}
}

func TestProgressPercent(t *testing.T) {
	tests := []struct {
		downloaded int64
		total      int64
		want       int
	}{
		{downloaded: 0, total: 100, want: 0},
		{downloaded: 67, total: 100, want: 67},
		{downloaded: 200, total: 100, want: 100},
		{downloaded: 10, total: -1, want: 0},
	}
	for _, test := range tests {
		if got := progressPercent(test.downloaded, test.total); got != test.want {
			t.Fatalf("progressPercent(%d, %d) = %d, want %d", test.downloaded, test.total, got, test.want)
		}
	}
}

func TestPrepareLatestRejectsChecksumMismatch(t *testing.T) {
	client, server, _ := testClient(t, []byte("tampered payload"), true)
	defer server.Close()

	_, err := client.PrepareLatest(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
}

func TestValidateGitHubURLRejectsUntrustedHost(t *testing.T) {
	if err := validateGitHubURL("https://github.com/HONG-LOU/entcoin/releases/download/v1.0.8/file"); err != nil {
		t.Fatal(err)
	}
	if err := validateGitHubURL("https://example.com/entcoin.exe"); err == nil {
		t.Fatal("untrusted host was accepted")
	}
	if err := validateGitHubURL("http://github.com/HONG-LOU/entcoin"); err == nil {
		t.Fatal("insecure GitHub URL was accepted")
	}
}

func TestValidateUpdateURLAllowsOnlyTheOfficialManifestOutsideGitHub(t *testing.T) {
	if err := validateUpdateURL(updateManifestURL); err != nil {
		t.Fatal(err)
	}
	if err := validateUpdateURL("https://entcoin.xyz/other.json"); err == nil {
		t.Fatal("unexpected Entcoin website URL was accepted")
	}
	if err := validateUpdateURL("https://entcoin.xyz/downloads/v1.0.15/entcoin_1.0.15_amd64.deb"); err != nil {
		t.Fatal(err)
	}
	if err := validateUpdateURL("https://template-chat.xyz/downloads/v1.0.15/entcoin_1.0.15_amd64.deb"); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{
		"http://entcoin.xyz/downloads/v1.0.15/entcoin.exe",
		"https://www.entcoin.xyz/downloads/v1.0.15/entcoin.exe",
		"https://entcoin.xyz/downloads/../update.json",
		"https://entcoin.xyz/downloads/v1.0.15/entcoin.exe?source=other",
		"https://template-chat.xyz/v2/status",
	} {
		if err := validateUpdateURL(address); err == nil {
			t.Fatalf("unexpected mirror URL was accepted: %s", address)
		}
	}
}

func TestNewPrefersAsianMirror(t *testing.T) {
	client := New()
	selection, err := client.selectRelease(
		testReleaseVersion,
		"https://github.com/HONG-LOU/entcoin/releases/tag/v"+testReleaseVersion,
		"2026-07-22T00:00:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := asianMirrorDownloadBase + "v" + testReleaseVersion + "/"
	if len(selection.artifact.URLs) != 3 || !strings.HasPrefix(selection.artifact.URLs[0], wantPrefix) {
		t.Fatalf("artifact sources = %v", selection.artifact.URLs)
	}
	_, checksumName, err := assetNames(client.platform, client.architecture, testReleaseVersion)
	if err != nil {
		t.Fatal(err)
	}
	githubChecksum := "https://github.com/HONG-LOU/entcoin/releases/download/v" + testReleaseVersion + "/" + checksumName
	if len(selection.checksum.URLs) != 1 || selection.checksum.URLs[0] != githubChecksum {
		t.Fatalf("checksum sources = %v", selection.checksum.URLs)
	}
}

func TestCheckPrefersWebsiteManifest(t *testing.T) {
	client, server, state := testClient(t, []byte("verified deb payload"), false)
	defer server.Close()

	status, err := client.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != testReleaseVersion {
		t.Fatalf("unexpected manifest status: %#v", status)
	}
	requests := state.requestPaths()
	if len(requests) != 1 || requests[0] != "/manifest" || containsString(requests, "/feed") {
		t.Fatalf("metadata requests = %v", requests)
	}
}

func TestCheckFallsBackToGitHubFeed(t *testing.T) {
	client, server, _ := testClient(t, []byte("verified deb payload"), false)
	defer server.Close()
	client.manifestURL = server.URL + "/missing-manifest"

	status, err := client.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != testReleaseVersion {
		t.Fatalf("unexpected feed status: %#v", status)
	}
}

func TestPrepareLatestUsesOnlyGitHubChecksum(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, state := testClient(t, artifact, false)
	defer server.Close()
	client.mirrorBases = []string{server.URL + "/mirror/"}

	if _, err := client.PrepareLatest(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	mirrorChecksum := "/mirror/v" + testReleaseVersion + "/SHA256SUMS-linux.txt"
	githubChecksum := "/download/v" + testReleaseVersion + "/SHA256SUMS-linux.txt"
	requests := state.requestPaths()
	if containsString(requests, mirrorChecksum) || !containsString(requests, githubChecksum) {
		t.Fatalf("checksum requests = %v", requests)
	}
}

func TestPrepareLatestRejectsCompromisedMirrorChecksum(t *testing.T) {
	trustedArtifact := []byte("verified deb payload")
	client, server, state := testClient(t, trustedArtifact, false)
	defer server.Close()
	client.mirrorBases = []string{server.URL + "/compromised-mirror/"}

	prepared, err := client.PrepareLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, trustedArtifact) {
		t.Fatalf("installed artifact = %q", data)
	}
	requests := state.requestPaths()
	compromisedChecksum := "/compromised-mirror/v" + testReleaseVersion + "/SHA256SUMS-linux.txt"
	if containsString(requests, compromisedChecksum) {
		t.Fatalf("compromised mirror was trusted for checksums: %v", requests)
	}
	paths, _ := state.artifactRequests()
	if len(paths) < 2 || !strings.HasPrefix(paths[0], "/compromised-mirror/") || !strings.HasPrefix(paths[1], "/download/") {
		t.Fatalf("artifact fallback order = %v", paths)
	}
}

func TestChecksumSourceTimeoutFallsBack(t *testing.T) {
	artifactName := "entcoin_" + testReleaseVersion + "_amd64.deb"
	checksum := sha256.Sum256([]byte("verified deb payload"))
	checksumText := hex.EncodeToString(checksum[:]) + "  " + artifactName + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			<-request.Context().Done()
			return
		}
		_, _ = response.Write([]byte(checksumText))
	}))
	defer server.Close()
	client := &Client{
		httpClient:      server.Client(),
		validateURL:     func(string) error { return nil },
		metadataTimeout: 20 * time.Millisecond,
	}

	expected, err := client.checksumForArtifact(context.Background(), releaseAsset{
		URLs: []string{server.URL + "/slow", server.URL + "/good"},
	}, artifactName)
	if err != nil {
		t.Fatal(err)
	}
	if expected != hex.EncodeToString(checksum[:]) {
		t.Fatalf("checksum = %q", expected)
	}
}

func TestChecksumRequiresAtLeastOneSource(t *testing.T) {
	client := &Client{}

	_, err := client.checksumForArtifact(context.Background(), releaseAsset{}, "entcoin.deb")
	if err == nil || !strings.Contains(err.Error(), "no checksum sources") {
		t.Fatalf("missing checksum source error = %v", err)
	}
}

func TestPrepareLatestFallsBackFromMirrorToGitHub(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, state := testClient(t, artifact, false)
	defer server.Close()
	client.mirrorBases = []string{server.URL + "/missing/"}

	prepared, err := client.PrepareLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prepared.Path); err != nil {
		t.Fatal(err)
	}
	paths, _ := state.artifactRequests()
	if len(paths) < 2 || !strings.HasPrefix(paths[0], "/missing/") || !strings.HasPrefix(paths[1], "/download/") {
		t.Fatalf("download source order = %v", paths)
	}
}

func TestPrepareLatestResumesPartialArtifact(t *testing.T) {
	artifact := []byte("verified deb payload with enough bytes to resume")
	client, server, state := testClient(t, artifact, false)
	defer server.Close()
	artifactName := "entcoin_" + testReleaseVersion + "_amd64.deb"
	partialPath := filepath.Join(client.cacheRoot, artifactName+".part")
	if err := os.WriteFile(partialPath, artifact[:12], 0o600); err != nil {
		t.Fatal(err)
	}

	prepared, err := client.PrepareLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, artifact) {
		t.Fatalf("resumed artifact = %q", data)
	}
	_, ranges := state.artifactRequests()
	if len(ranges) != 1 || ranges[0] != "bytes=12-" {
		t.Fatalf("artifact ranges = %v", ranges)
	}
}

func TestPrepareLatestRestartsWhenServerIgnoresRange(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, _ := testClient(t, artifact, false)
	defer server.Close()
	client.downloadBase = server.URL + "/no-range/"
	artifactName := "entcoin_" + testReleaseVersion + "_amd64.deb"
	partialPath := filepath.Join(client.cacheRoot, artifactName+".part")
	if err := os.WriteFile(partialPath, []byte("old partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	prepared, err := client.PrepareLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, artifact) {
		t.Fatalf("restarted artifact = %q", data)
	}
}

func TestPrepareLatestRejectsBadMirrorAndDownloadsCleanFallback(t *testing.T) {
	artifact := []byte("verified deb payload")
	client, server, state := testClient(t, artifact, false)
	defer server.Close()
	client.mirrorBases = []string{server.URL + "/bad-mirror/"}

	prepared, err := client.PrepareLatest(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, artifact) {
		t.Fatalf("fallback artifact = %q", data)
	}
	paths, _ := state.artifactRequests()
	if len(paths) < 2 || !strings.HasPrefix(paths[0], "/bad-mirror/") || !strings.HasPrefix(paths[1], "/download/") {
		t.Fatalf("download source order = %v", paths)
	}
}

func TestReadRetriesTemporaryFailures(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(response, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = response.Write([]byte("ready"))
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), validateURL: func(string) error { return nil }}

	data, err := client.read(context.Background(), server.URL, 32)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ready" || attempts != 3 {
		t.Fatalf("read = %q after %d attempts", data, attempts)
	}
}

type testServerState struct {
	mu       sync.Mutex
	paths    []string
	ranges   []string
	requests []string
}

func (s *testServerState) recordRequest(path, rangeHeader string, artifact bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, path)
	if artifact {
		s.paths = append(s.paths, path)
		s.ranges = append(s.ranges, rangeHeader)
	}
}

func (s *testServerState) requestPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *testServerState) artifactRequests() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.ranges...)
}

func testClient(t *testing.T, artifact []byte, mismatch bool) (*Client, *httptest.Server, *testServerState) {
	t.Helper()
	artifactName := "entcoin_" + testReleaseVersion + "_amd64.deb"
	checksum := sha256.Sum256(artifact)
	checksumText := hex.EncodeToString(checksum[:])
	if mismatch {
		checksumText = strings.Repeat("0", sha256.Size*2)
	}
	state := &testServerState{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		state.recordRequest(
			request.URL.Path,
			request.Header.Get("Range"),
			strings.Contains(request.URL.Path, artifactName),
		)
		switch request.URL.Path {
		case "/feed":
			response.Header().Set("Content-Type", "application/atom+xml")
			_ = xml.NewEncoder(response).Encode(atomFeed{Entries: []atomEntry{
				{
					Title:   "v" + testReleaseVersion,
					Updated: "2026-07-22T00:00:00Z",
					Links: []atomLink{{
						Relation: "alternate",
						Type:     "text/html",
						Address:  "https://github.com/HONG-LOU/entcoin/releases/tag/v" + testReleaseVersion,
					}},
				},
			}})
		case "/manifest":
			_, _ = response.Write([]byte(`{"version":"` + testReleaseVersion + `","published_at":"2026-07-22T00:00:00Z","release_url":"https://github.com/HONG-LOU/entcoin/releases/tag/v` + testReleaseVersion + `"}`))
		case "/invalid-feed":
			_, _ = response.Write([]byte("not atom"))
		case "/download/v" + testReleaseVersion + "/" + artifactName:
			http.ServeContent(response, request, artifactName, time.Time{}, bytes.NewReader(artifact))
		case "/mirror/v" + testReleaseVersion + "/" + artifactName,
			"/bad-checksum/v" + testReleaseVersion + "/" + artifactName:
			http.ServeContent(response, request, artifactName, time.Time{}, bytes.NewReader(artifact))
		case "/no-range/v" + testReleaseVersion + "/" + artifactName:
			_, _ = response.Write(artifact)
		case "/bad-mirror/v" + testReleaseVersion + "/" + artifactName:
			_, _ = response.Write(bytes.Repeat([]byte("x"), len(artifact)))
		case "/compromised-mirror/v" + testReleaseVersion + "/" + artifactName:
			_, _ = response.Write([]byte("malicious payload"))
		case "/download/v" + testReleaseVersion + "/SHA256SUMS-linux.txt":
			_, _ = response.Write([]byte(checksumText + "  " + artifactName + "\n"))
		case "/mirror/v" + testReleaseVersion + "/SHA256SUMS-linux.txt":
			_, _ = response.Write([]byte(checksumText + "  " + artifactName + "\n"))
		case "/compromised-mirror/v" + testReleaseVersion + "/SHA256SUMS-linux.txt":
			malicious := sha256.Sum256([]byte("malicious payload"))
			_, _ = response.Write([]byte(hex.EncodeToString(malicious[:]) + "  " + artifactName + "\n"))
		case "/no-range/v" + testReleaseVersion + "/SHA256SUMS-linux.txt":
			_, _ = response.Write([]byte(checksumText + "  " + artifactName + "\n"))
		default:
			http.NotFound(response, request)
		}
	}))
	client := &Client{
		feedURL:         server.URL + "/feed",
		manifestURL:     server.URL + "/manifest",
		downloadBase:    server.URL + "/download/",
		httpClient:      server.Client(),
		platform:        "linux",
		architecture:    "amd64",
		cacheRoot:       t.TempDir(),
		validateURL:     func(string) error { return nil },
		metadataTimeout: time.Second,
	}
	return client, server, state
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
