package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	CurrentVersion          = "1.5.2"
	ReleasesURL             = "https://github.com/HONG-LOU/entcoin/releases/latest"
	releaseFeedURL          = "https://github.com/HONG-LOU/entcoin/releases.atom"
	updateManifestURL       = "https://entcoin.xyz/update.json"
	asianMirrorDownloadBase = "https://template-chat.xyz/downloads/"
	mirrorDownloadBase      = "https://entcoin.xyz/downloads/"
	maxMetadataBytes        = 1 << 20
	maxChecksumBytes        = 128 << 10
	maxArtifactBytes        = 300 << 20
	defaultMetadataTimeout  = 10 * time.Second
)

type Status struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Available      bool   `json:"available"`
	ReleaseURL     string `json:"release_url"`
	PublishedAt    string `json:"published_at"`
	AssetName      string `json:"asset_name"`
}

type PreparedUpdate struct {
	Status Status
	Path   string
}

type Progress struct {
	Phase      string `json:"phase"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Percent    int    `json:"percent"`
}

type ProgressReporter func(Progress)

type Client struct {
	feedURL         string
	manifestURL     string
	downloadBase    string
	mirrorBases     []string
	httpClient      *http.Client
	platform        string
	architecture    string
	cacheRoot       string
	validateURL     func(string) error
	metadataTimeout time.Duration
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	Updated string     `xml:"updated"`
	Links   []atomLink `xml:"link"`
}

type atomLink struct {
	Relation string `xml:"rel,attr"`
	Type     string `xml:"type,attr"`
	Address  string `xml:"href,attr"`
}

type updateManifest struct {
	Version     string `json:"version"`
	PublishedAt string `json:"published_at"`
	ReleaseURL  string `json:"release_url"`
}

type releaseAsset struct {
	Name string
	URLs []string
}

type releaseSelection struct {
	status   Status
	artifact releaseAsset
	checksum releaseAsset
}

func New() *Client {
	return &Client{
		feedURL:         releaseFeedURL,
		manifestURL:     updateManifestURL,
		downloadBase:    "https://github.com/HONG-LOU/entcoin/releases/download/",
		mirrorBases:     []string{asianMirrorDownloadBase, mirrorDownloadBase},
		httpClient:      secureHTTPClient(),
		platform:        runtime.GOOS,
		architecture:    runtime.GOARCH,
		validateURL:     validateUpdateURL,
		metadataTimeout: defaultMetadataTimeout,
	}
}

func (c *Client) Check(ctx context.Context) (Status, error) {
	selection, err := c.latest(ctx)
	if err != nil {
		return Status{}, err
	}
	return selection.status, nil
}

func (c *Client) PrepareLatest(ctx context.Context, report ProgressReporter) (PreparedUpdate, error) {
	reportProgress(report, "preparing", 0, 0)
	selection, err := c.latest(ctx)
	if err != nil {
		return PreparedUpdate{}, err
	}
	if !selection.status.Available {
		return PreparedUpdate{}, errors.New("Entcoin is already up to date")
	}
	reportProgress(report, "checking", 0, 0)
	expected, err := c.checksumForArtifact(ctx, selection.checksum, selection.artifact.Name)
	if err != nil {
		return PreparedUpdate{}, fmt.Errorf("download release checksums: %w", err)
	}
	path, err := c.downloadArtifact(ctx, selection.artifact, expected, report)
	if err != nil {
		return PreparedUpdate{}, err
	}
	return PreparedUpdate{Status: selection.status, Path: path}, nil
}

func (c *Client) latest(ctx context.Context) (releaseSelection, error) {
	manifestContext, cancelManifest := context.WithTimeout(ctx, c.metadataDeadline())
	selection, manifestErr := c.latestFromManifest(manifestContext)
	cancelManifest()
	if manifestErr == nil {
		return selection, nil
	}
	feedContext, cancelFeed := context.WithTimeout(ctx, c.metadataDeadline())
	selection, feedErr := c.latestFromFeed(feedContext)
	cancelFeed()
	if feedErr == nil {
		return selection, nil
	}
	return releaseSelection{}, fmt.Errorf("check latest release: %w", errors.Join(manifestErr, feedErr))
}

func (c *Client) latestFromFeed(ctx context.Context) (releaseSelection, error) {
	data, err := c.read(ctx, c.feedURL, maxMetadataBytes)
	if err != nil {
		return releaseSelection{}, fmt.Errorf("read GitHub release feed: %w", err)
	}
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return releaseSelection{}, fmt.Errorf("decode release feed: %w", err)
	}
	entry, latestVersion, err := latestStableEntry(feed.Entries)
	if err != nil {
		return releaseSelection{}, err
	}
	releaseURL, err := entry.releaseURL(latestVersion)
	if err != nil {
		return releaseSelection{}, err
	}
	return c.selectRelease(latestVersion, releaseURL, entry.Updated)
}

func (c *Client) latestFromManifest(ctx context.Context) (releaseSelection, error) {
	if c.manifestURL == "" {
		return releaseSelection{}, errors.New("update manifest fallback is not configured")
	}
	data, err := c.read(ctx, c.manifestURL, maxMetadataBytes)
	if err != nil {
		return releaseSelection{}, fmt.Errorf("read Entcoin update manifest: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest updateManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseSelection{}, fmt.Errorf("decode update manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return releaseSelection{}, fmt.Errorf("decode update manifest: %w", err)
	}
	if err := validateGitHubReleaseURL(manifest.ReleaseURL, manifest.Version); err != nil {
		return releaseSelection{}, err
	}
	return c.selectRelease(manifest.Version, manifest.ReleaseURL, manifest.PublishedAt)
}

func (c *Client) selectRelease(latestVersion, releaseURL, publishedAt string) (releaseSelection, error) {
	comparison, err := compareVersions(latestVersion, CurrentVersion)
	if err != nil {
		return releaseSelection{}, fmt.Errorf("validate release version: %w", err)
	}
	status := Status{
		CurrentVersion: CurrentVersion,
		LatestVersion:  latestVersion,
		Available:      comparison > 0,
		ReleaseURL:     releaseURL,
		PublishedAt:    publishedAt,
	}
	if !status.Available {
		return releaseSelection{status: status}, nil
	}
	artifactName, checksumName, err := assetNames(c.platform, c.architecture, latestVersion)
	if err != nil {
		return releaseSelection{}, err
	}
	githubBase := c.downloadBase + "v" + latestVersion + "/"
	artifactURLs := make([]string, 0, len(c.mirrorBases)+1)
	for _, base := range c.mirrorBases {
		artifactURLs = append(artifactURLs, base+"v"+latestVersion+"/"+artifactName)
	}
	artifactURLs = append(artifactURLs, githubBase+artifactName)
	artifact := releaseAsset{Name: artifactName, URLs: artifactURLs}
	// Mirrors distribute bytes but are not update trust roots. Fetching both an
	// artifact and its checksum from one mirror would let a compromised mirror
	// replace both and pass verification.
	checksum := releaseAsset{Name: checksumName, URLs: []string{githubBase + checksumName}}
	status.AssetName = artifactName
	return releaseSelection{status: status, artifact: artifact, checksum: checksum}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func (c *Client) read(ctx context.Context, address string, limit int64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 250 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		data, err := c.readOnce(ctx, address, limit)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *Client) checksumForArtifact(ctx context.Context, checksum releaseAsset, artifactName string) (string, error) {
	if len(checksum.URLs) == 0 {
		return "", errors.New("no checksum sources are configured")
	}
	var sourceErrors []error
	for _, address := range checksum.URLs {
		sourceContext, cancel := context.WithTimeout(ctx, c.metadataDeadline())
		data, err := c.read(sourceContext, address, maxChecksumBytes)
		cancel()
		if err == nil {
			var expected string
			expected, err = checksumFor(data, artifactName)
			if err == nil {
				return expected, nil
			}
		}
		sourceErrors = append(sourceErrors, fmt.Errorf("%s: %w", downloadHost(address), err))
	}
	return "", errors.Join(sourceErrors...)
}

func (c *Client) metadataDeadline() time.Duration {
	if c.metadataTimeout > 0 {
		return c.metadataTimeout
	}
	return defaultMetadataTimeout
}

func (c *Client) readOnce(ctx context.Context, address string, limit int64) ([]byte, error) {
	if err := c.validateURL(address); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, application/atom+xml, application/octet-stream")
	request.Header.Set("User-Agent", "Entcoin/"+CurrentVersion)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func (c *Client) downloadArtifact(
	ctx context.Context,
	asset releaseAsset,
	expected string,
	report ProgressReporter,
) (string, error) {
	cacheRoot := c.cacheRoot
	if cacheRoot == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("find user cache directory: %w", err)
		}
		cacheRoot = filepath.Join(userCache, "Entcoin", "updates")
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return "", fmt.Errorf("create update cache: %w", err)
	}
	destination := filepath.Join(cacheRoot, asset.Name)
	if valid, err := fileMatches(destination, expected); err != nil {
		return "", err
	} else if valid {
		reportProgress(report, "verifying", 1, 1)
		return destination, nil
	}
	partialPath := destination + ".part"
	if valid, err := fileMatches(partialPath, expected); err != nil {
		return "", err
	} else if valid {
		info, err := os.Stat(partialPath)
		if err != nil {
			return "", fmt.Errorf("inspect verified update: %w", err)
		}
		reportProgress(report, "verifying", info.Size(), info.Size())
		return promoteDownloadedArtifact(partialPath, destination)
	}

	var downloadErrors []error
	for _, address := range asset.URLs {
		if err := c.downloadSource(ctx, address, partialPath, report); err != nil {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: %w", downloadHost(address), err))
			continue
		}
		valid, err := fileMatches(partialPath, expected)
		if err != nil {
			return "", err
		}
		if !valid {
			downloadErrors = append(downloadErrors, fmt.Errorf("%s: downloaded update failed SHA-256 verification", downloadHost(address)))
			if err := os.Truncate(partialPath, 0); err != nil {
				return "", fmt.Errorf("reset invalid partial update: %w", err)
			}
			continue
		}
		return promoteDownloadedArtifact(partialPath, destination)
	}
	return "", fmt.Errorf("download update: %w", errors.Join(downloadErrors...))
}

func (c *Client) downloadSource(ctx context.Context, address, partialPath string, report ProgressReporter) error {
	if err := c.validateURL(address); err != nil {
		return err
	}
	partial, err := os.OpenFile(partialPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open partial update: %w", err)
	}
	defer partial.Close()

	for attempt := 0; attempt < 2; attempt++ {
		info, err := partial.Stat()
		if err != nil {
			return fmt.Errorf("inspect partial update: %w", err)
		}
		offset := info.Size()
		if offset > maxArtifactBytes {
			if err := partial.Truncate(0); err != nil {
				return fmt.Errorf("reset oversized partial update: %w", err)
			}
			offset = 0
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/octet-stream")
		request.Header.Set("User-Agent", "Entcoin/"+CurrentVersion)
		if offset > 0 {
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return fmt.Errorf("request update: %w", err)
		}

		if response.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
			response.Body.Close()
			if err := partial.Truncate(0); err != nil {
				return fmt.Errorf("reset rejected partial update: %w", err)
			}
			continue
		}

		total, appendResponse, err := downloadResponseRange(response, offset)
		if err != nil {
			response.Body.Close()
			return err
		}
		if total > maxArtifactBytes {
			response.Body.Close()
			return errors.New("response exceeds the maximum artifact size")
		}
		if !appendResponse {
			offset = 0
			if err := partial.Truncate(0); err != nil {
				response.Body.Close()
				return fmt.Errorf("restart partial update: %w", err)
			}
		}
		if _, err := partial.Seek(offset, io.SeekStart); err != nil {
			response.Body.Close()
			return fmt.Errorf("seek partial update: %w", err)
		}
		reportProgress(report, "downloading", offset, total)
		progress := &progressWriter{writer: partial, total: total, downloaded: offset, report: report}
		written, copyErr := io.Copy(progress, io.LimitReader(response.Body, maxArtifactBytes-offset+1))
		closeErr := response.Body.Close()
		if copyErr != nil {
			return fmt.Errorf("write partial update: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close update response: %w", closeErr)
		}
		if offset+written > maxArtifactBytes || (response.ContentLength >= 0 && written != response.ContentLength) {
			return errors.New("downloaded update size does not match the HTTP response")
		}
		if total >= 0 && offset+written != total {
			return errors.New("partial update did not reach the advertised size")
		}
		if err := partial.Sync(); err != nil {
			return fmt.Errorf("flush partial update: %w", err)
		}
		reportProgress(report, "verifying", offset+written, total)
		return nil
	}
	return errors.New("server rejected the partial update")
}

var contentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func downloadResponseRange(response *http.Response, offset int64) (int64, bool, error) {
	if response.StatusCode == http.StatusOK {
		return response.ContentLength, false, nil
	}
	if response.StatusCode != http.StatusPartialContent || offset == 0 {
		return 0, false, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	matches := contentRangePattern.FindStringSubmatch(response.Header.Get("Content-Range"))
	if matches == nil {
		return 0, false, errors.New("invalid Content-Range response")
	}
	start, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, false, errors.New("invalid Content-Range start")
	}
	end, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return 0, false, errors.New("invalid Content-Range end")
	}
	total, err := strconv.ParseInt(matches[3], 10, 64)
	if err != nil {
		return 0, false, errors.New("invalid Content-Range total")
	}
	if start != offset || end < start || end >= total || (response.ContentLength >= 0 && response.ContentLength != end-start+1) {
		return 0, false, errors.New("inconsistent Content-Range response")
	}
	return total, true, nil
}

func promoteDownloadedArtifact(partialPath, destination string) (string, error) {
	if err := os.Chmod(partialPath, 0o700); err != nil {
		return "", fmt.Errorf("secure update file: %w", err)
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("replace cached update: %w", err)
	}
	if err := os.Rename(partialPath, destination); err != nil {
		return "", fmt.Errorf("store verified update: %w", err)
	}
	return destination, nil
}

func downloadHost(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "update source"
	}
	return parsed.Hostname()
}

type progressWriter struct {
	writer      io.Writer
	total       int64
	downloaded  int64
	lastPercent int
	report      ProgressReporter
}

func (w *progressWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.downloaded += int64(written)
	percent := progressPercent(w.downloaded, w.total)
	if percent != w.lastPercent || w.total <= 0 {
		w.lastPercent = percent
		reportProgress(w.report, "downloading", w.downloaded, w.total)
	}
	return written, err
}

func reportProgress(reporter ProgressReporter, phase string, downloaded, total int64) {
	if reporter == nil {
		return
	}
	reporter(Progress{
		Phase:      phase,
		Downloaded: downloaded,
		Total:      total,
		Percent:    progressPercent(downloaded, total),
	})
}

func progressPercent(downloaded, total int64) int {
	if total <= 0 || downloaded <= 0 {
		return 0
	}
	if downloaded >= total {
		return 100
	}
	return int(downloaded * 100 / total)
}

func secureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			return validateUpdateURL(request.URL.String())
		},
	}
}

func validateUpdateURL(raw string) error {
	if raw == updateManifestURL {
		return nil
	}
	if err := validateMirrorURL(raw); err == nil {
		return nil
	}
	return validateGitHubURL(raw)
}

var mirrorPathPattern = regexp.MustCompile(`^/downloads/v[0-9]+\.[0-9]+\.[0-9]+/[A-Za-z0-9._-]+$`)

func validateMirrorURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	trustedHost := parsed.Host == "entcoin.xyz" || parsed.Host == "template-chat.xyz"
	if parsed.Scheme != "https" || parsed.User != nil || !trustedHost ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !mirrorPathPattern.MatchString(parsed.EscapedPath()) {
		return errors.New("update URL is not an official Entcoin mirror URL")
	}
	return nil
}

func validateGitHubURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(parsed.Hostname())
	trusted := host == "github.com" || host == "api.github.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
	if parsed.Scheme != "https" || parsed.User != nil || !trusted {
		return errors.New("release URL is not a trusted GitHub HTTPS URL")
	}
	return nil
}

func validateGitHubReleaseURL(raw, version string) error {
	if err := validateGitHubURL(raw); err != nil {
		return fmt.Errorf("validate release page URL: %w", err)
	}
	parsed, _ := url.Parse(raw)
	expectedPath := "/HONG-LOU/entcoin/releases/tag/v" + version
	if parsed.Hostname() != "github.com" || parsed.EscapedPath() != expectedPath {
		return errors.New("release page URL does not belong to Entcoin")
	}
	return nil
}

func compareVersions(left, right string) (int, error) {
	leftParts, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1, nil
		}
		if leftParts[index] > rightParts[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(version string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(result) {
		return result, fmt.Errorf("version %q must contain three numeric components", version)
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return result, fmt.Errorf("version %q is not canonical", version)
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return result, fmt.Errorf("version %q is invalid", version)
		}
		result[index] = value
	}
	return result, nil
}

func assetNames(platform, architecture, version string) (string, string, error) {
	if architecture != "amd64" {
		return "", "", fmt.Errorf("automatic updates do not support %s/%s", platform, architecture)
	}
	switch platform {
	case "linux":
		return "entcoin_" + version + "_amd64.deb", "SHA256SUMS-linux.txt", nil
	case "windows":
		return "Entcoin.exe", "SHA256SUMS.txt", nil
	default:
		return "", "", fmt.Errorf("automatic updates do not support %s/%s", platform, architecture)
	}
}

func checksumFor(data []byte, filename string) (string, error) {
	match := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("checksum list contains duplicate entry for %s", filename)
		}
		candidate := strings.ToLower(fields[0])
		decoded, err := hex.DecodeString(candidate)
		if err != nil || len(decoded) != sha256.Size {
			return "", fmt.Errorf("checksum for %s is invalid", filename)
		}
		match = candidate
	}
	if match == "" {
		return "", fmt.Errorf("checksum list does not contain %s", filename)
	}
	return match, nil
}

func fileMatches(path, expected string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect cached update: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxArtifactBytes {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open cached update: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("verify cached update: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)) == expected, nil
}

func latestStableEntry(entries []atomEntry) (atomEntry, string, error) {
	var selected atomEntry
	selectedVersion := ""
	for _, entry := range entries {
		version := strings.TrimPrefix(strings.TrimSpace(entry.Title), "v")
		if _, err := parseVersion(version); err != nil {
			continue
		}
		if selectedVersion == "" {
			selected = entry
			selectedVersion = version
			continue
		}
		comparison, _ := compareVersions(version, selectedVersion)
		if comparison > 0 {
			selected = entry
			selectedVersion = version
		}
	}
	if selectedVersion == "" {
		return atomEntry{}, "", errors.New("release feed does not contain a stable version")
	}
	return selected, selectedVersion, nil
}

func (entry atomEntry) releaseURL(version string) (string, error) {
	for _, link := range entry.Links {
		if link.Relation != "alternate" || link.Type != "text/html" {
			continue
		}
		if err := validateGitHubReleaseURL(link.Address, version); err != nil {
			return "", err
		}
		return link.Address, nil
	}
	return "", errors.New("stable release is missing its GitHub page")
}
