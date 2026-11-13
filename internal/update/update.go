// SPDX-License-Identifier: MPL-2.0

package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultAPIURL          = "https://api.github.com"
	DefaultDownloadURL     = "https://github.com"
	DefaultRepository      = "Lefalock/clients-cli"
	AutomaticCheckInterval = 10 * time.Minute
	apiTimeout             = 15 * time.Second
	downloadTimeout        = 10 * time.Minute
	maxArchiveSize         = 128 << 20
	maxChecksumSize        = 1 << 20
	maxBinarySize          = 128 << 20
)

var versionPattern = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

var ErrDevelopmentBuild = errors.New("updates are available only for release builds with a vX.Y.Z version")

type Config struct {
	APIURL      string
	DownloadURL string
	Repository  string
	Version     string
	DataDir     string
	HTTPClient  *http.Client
	Now         func() time.Time
}

type Result struct {
	Current      string
	Latest       string
	Available    bool
	Deferred     bool
	InstallError string
}

type release struct {
	TagName    string      `json:"tag_name"`
	Draft      bool        `json:"draft"`
	Prerelease bool        `json:"prerelease"`
	Assets     []assetLink `json:"assets"`
}

type assetLink struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	State       string `json:"state"`
}

type state struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

type Manager struct {
	baseURL     *url.URL
	downloadURL *url.URL
	repository  string
	version     string
	dataDir     string
	client      *http.Client
	now         func() time.Time
	executable  func() (string, error)
	replace     func(source, target, statusPath string) (bool, error)
}

func (m *Manager) CurrentVersion() string {
	return m.version
}

func New(config Config) (*Manager, error) {
	parseBase := func(value, fallback string) (*url.URL, error) {
		if value == "" {
			value = fallback
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid GitHub URL %q", value)
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		return parsed, nil
	}
	base, err := parseBase(config.APIURL, DefaultAPIURL)
	if err != nil {
		return nil, err
	}
	downloads, err := parseBase(config.DownloadURL, DefaultDownloadURL)
	if err != nil {
		return nil, err
	}
	repository := config.Repository
	if repository == "" {
		repository = DefaultRepository
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) || strings.HasSuffix(repository, "/.") || strings.HasSuffix(repository, "/..") {
		return nil, errors.New("invalid GitHub repository")
	}
	if config.DataDir == "" {
		return nil, errors.New("update data directory is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = secureHTTPClient(0)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{baseURL: base, downloadURL: downloads, repository: repository, version: config.Version, dataDir: config.DataDir, client: client, now: now, executable: os.Executable, replace: replaceExecutable}, nil
}

func secureHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			origin := via[0].URL
			if origin.Scheme == "https" && request.URL.Scheme != "https" {
				return errors.New("refusing HTTPS downgrade during update download")
			}
			if !sameOrigin(origin, request.URL) {
				request.Header.Del("Authorization")
				request.Header.Del("PRIVATE-TOKEN")
			}
			return nil
		},
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (m *Manager) Check(ctx context.Context) (Result, error) {
	if err := m.previousInstallError(); err != nil {
		return Result{}, err
	}
	if !versionPattern.MatchString(m.version) {
		return Result{}, ErrDevelopmentBuild
	}
	latest, err := m.latestRelease(ctx)
	if err != nil {
		return Result{}, err
	}
	comparison, err := compareVersions(m.version, latest.TagName)
	if err != nil {
		return Result{}, err
	}
	result := Result{Current: m.version, Latest: latest.TagName, Available: comparison < 0}
	if err := m.writeState(state{CheckedAt: m.now().UTC(), Latest: latest.TagName}); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (m *Manager) AutomaticCheck(ctx context.Context) (Result, bool, error) {
	if message, err := m.consumeInstallStatus(); err != nil {
		return Result{}, false, err
	} else if message != "" {
		return Result{InstallError: message}, true, nil
	}
	if !versionPattern.MatchString(m.version) {
		return Result{}, false, nil
	}
	stored, err := m.readState()
	if err != nil {
		return Result{}, false, err
	}
	now := m.now()
	cacheValid := stored.Latest != "" && versionPattern.MatchString(stored.Latest) &&
		!stored.CheckedAt.IsZero() && !stored.CheckedAt.After(now.Add(5*time.Minute)) &&
		now.Before(stored.CheckedAt.Add(AutomaticCheckInterval))
	if cacheValid {
		comparison, compareErr := compareVersions(m.version, stored.Latest)
		if compareErr != nil {
			return Result{}, false, compareErr
		}
		return Result{Current: m.version, Latest: stored.Latest, Available: comparison < 0}, true, nil
	}
	result, err := m.Check(ctx)
	return result, err == nil, err
}

func (m *Manager) Install(ctx context.Context) (Result, error) {
	if err := m.previousInstallError(); err != nil {
		return Result{}, err
	}
	if !versionPattern.MatchString(m.version) {
		return Result{}, ErrDevelopmentBuild
	}
	latest, err := m.latestRelease(ctx)
	if err != nil {
		return Result{}, err
	}
	comparison, err := compareVersions(m.version, latest.TagName)
	if err != nil {
		return Result{}, err
	}
	result := Result{Current: m.version, Latest: latest.TagName, Available: comparison < 0}
	if !result.Available {
		return result, nil
	}

	artifact, checksumName, directPath, err := selectArtifact(latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Result{}, err
	}
	checksumPath, err := findAsset(latest, "SHA256SUMS")
	if err != nil {
		return Result{}, err
	}
	workDir, err := os.MkdirTemp(filepath.Join(m.dataDir, "updates"), ".download-")
	if err != nil {
		if mkdirErr := os.MkdirAll(filepath.Join(m.dataDir, "updates"), 0o700); mkdirErr != nil {
			return Result{}, fmt.Errorf("create update directory: %w", mkdirErr)
		}
		workDir, err = os.MkdirTemp(filepath.Join(m.dataDir, "updates"), ".download-")
	}
	if err != nil {
		return Result{}, fmt.Errorf("create update workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	checksumData, err := m.download(ctx, latest.TagName, "SHA256SUMS", checksumPath, maxChecksumSize)
	if err != nil {
		return Result{}, fmt.Errorf("download checksums: %w", err)
	}
	expected, err := checksumFor(checksumData, checksumName)
	if err != nil {
		return Result{}, err
	}
	archiveData, err := m.download(ctx, latest.TagName, checksumName, directPath, maxArchiveSize)
	if err != nil {
		return Result{}, fmt.Errorf("download %s: %w", artifact, err)
	}
	actual := sha256.Sum256(archiveData)
	if !strings.EqualFold(expected, hex.EncodeToString(actual[:])) {
		return Result{}, fmt.Errorf("checksum mismatch for %s", artifact)
	}
	binaryPath, err := extractBinary(workDir, artifact, archiveData)
	if err != nil {
		return Result{}, err
	}
	executable, err := m.executable()
	if err != nil {
		return Result{}, fmt.Errorf("locate current executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	deferred, err := m.replace(binaryPath, executable, m.installStatusPath())
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return Result{}, fmt.Errorf("replace %s: permission denied after the release was downloaded and verified; update the system installation as an administrator or install the CLI in a user-writable directory such as ~/.local/bin", executable)
		}
		return Result{}, fmt.Errorf("replace %s: %w", executable, err)
	}
	result.Deferred = deferred
	return result, nil
}

func (m *Manager) latestRelease(ctx context.Context) (release, error) {
	requestContext, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	endpoint := strings.TrimRight(m.baseURL.String(), "/") + "/repos/" + m.repository + "/releases/latest"
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Lefalock/"+m.version)
	response, err := m.client.Do(request)
	if err != nil {
		return release{}, fmt.Errorf("check latest GitHub release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return release{}, errors.New("no public GitHub release is available yet")
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		return release{}, errors.New("GitHub update request was denied or rate limited; try again later")
	}
	if response.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("check latest GitHub release: HTTP %d", response.StatusCode)
	}
	var value release
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&value); err != nil {
		return release{}, fmt.Errorf("decode latest GitHub release: %w", err)
	}
	if value.Draft || value.Prerelease || !versionPattern.MatchString(value.TagName) {
		return release{}, fmt.Errorf("GitHub returned an unsupported release %q", value.TagName)
	}
	return value, nil
}

func (m *Manager) download(ctx context.Context, tag, name, endpoint string, maximum int64) ([]byte, error) {
	expected := strings.TrimRight(m.downloadURL.String(), "/") + "/" + m.repository + "/releases/download/" + tag + "/" + name
	if !versionPattern.MatchString(tag) || filepath.Base(name) != name || endpoint != expected {
		return nil, errors.New("release contains an unexpected asset URL")
	}
	requestContext, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Lefalock/"+m.version)
	response, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maximum {
		return nil, errors.New("release asset exceeds the allowed size")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("release asset exceeds the allowed size")
	}
	return data, nil
}

func selectArtifact(value release, goos, goarch string) (string, string, string, error) {
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	name := "lefalock_" + value.TagName + "_" + goos + "_" + goarch + extension
	endpoint, err := findAsset(value, name)
	if err != nil {
		return "", "", "", fmt.Errorf("no release binary for %s/%s: %w", goos, goarch, err)
	}
	return name, name, endpoint, nil
}

func findAsset(value release, name string) (string, error) {
	var endpoint string
	for _, asset := range value.Assets {
		if asset.Name != name {
			continue
		}
		if endpoint != "" {
			return "", fmt.Errorf("duplicate release asset %s", name)
		}
		if asset.State != "uploaded" || asset.DownloadURL == "" {
			return "", fmt.Errorf("release asset %s is incomplete", name)
		}
		endpoint = asset.DownloadURL
	}
	if endpoint == "" {
		return "", fmt.Errorf("release asset %s is missing", name)
	}
	return endpoint, nil
}

func checksumFor(data []byte, filename string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != filename {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			break
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			break
		}
		return strings.ToLower(fields[0]), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("SHA-256 checksum for %s is missing or invalid", filename)
}

func extractBinary(directory, archiveName string, data []byte) (string, error) {
	destination := filepath.Join(directory, executableName())
	if strings.HasSuffix(archiveName, ".tar.gz") {
		compressed, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("open release archive: %w", err)
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		found := false
		readme := false
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read release archive: %w", err)
			}
			if header.Name == "README.md" && header.Typeflag == tar.TypeReg && !readme && header.Size >= 0 && header.Size <= 1<<20 {
				readme = true
				continue
			}
			if header.Name != executableName() || header.Typeflag != tar.TypeReg || found {
				return "", errors.New("release archive contains unexpected files")
			}
			if header.Size < 0 || header.Size > maxBinarySize {
				return "", errors.New("release binary exceeds the allowed size")
			}
			if err := writeBinary(destination, io.LimitReader(reader, maxBinarySize+1)); err != nil {
				return "", err
			}
			found = true
		}
		if !found {
			return "", errors.New("release archive does not contain lefalock")
		}
		return destination, nil
	}
	if strings.HasSuffix(archiveName, ".zip") {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("open release archive: %w", err)
		}
		var binary *zip.File
		readme := false
		for _, entry := range reader.File {
			if entry.FileInfo().Mode()&os.ModeType != 0 {
				return "", errors.New("release archive contains unexpected files")
			}
			switch entry.Name {
			case executableName():
				if binary != nil {
					return "", errors.New("duplicate release binary")
				}
				binary = entry
			case "README.md":
				if readme || entry.UncompressedSize64 > 1<<20 {
					return "", errors.New("invalid release README")
				}
				readme = true
			default:
				return "", errors.New("release archive contains unexpected files")
			}
		}
		if binary == nil {
			return "", errors.New("release archive does not contain lefalock")
		}
		if binary.UncompressedSize64 > maxBinarySize {
			return "", errors.New("release binary exceeds the allowed size")
		}
		file, err := binary.Open()
		if err != nil {
			return "", fmt.Errorf("open release binary: %w", err)
		}
		defer file.Close()
		if err := writeBinary(destination, io.LimitReader(file, maxBinarySize+1)); err != nil {
			return "", err
		}
		return destination, nil
	}
	return "", errors.New("unsupported release archive format")
}

func writeBinary(path string, source io.Reader) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return fmt.Errorf("create extracted binary: %w", err)
	}
	written, copyErr := io.Copy(file, source)
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("extract release binary: %w", copyErr)
	}
	if written > maxBinarySize {
		return errors.New("release binary exceeds the allowed size")
	}
	if syncErr != nil {
		return fmt.Errorf("sync extracted binary: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close extracted binary: %w", closeErr)
	}
	return nil
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "lefalock.exe"
	}
	return "lefalock"
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

func parseVersion(value string) ([3]uint64, error) {
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return [3]uint64{}, fmt.Errorf("invalid release version %q", value)
	}
	var result [3]uint64
	for index := range result {
		if _, err := fmt.Sscan(match[index+1], &result[index]); err != nil {
			return [3]uint64{}, fmt.Errorf("invalid release version %q", value)
		}
	}
	return result, nil
}

func (m *Manager) statePath() string { return filepath.Join(m.dataDir, "update-state.json") }

func (m *Manager) readState() (state, error) {
	data, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("read update state: %w", err)
	}
	var value state
	if err := json.Unmarshal(data, &value); err != nil {
		if removeErr := os.Remove(m.statePath()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return state{}, fmt.Errorf("remove invalid update state: %w", removeErr)
		}
		return state{}, nil
	}
	return value, nil
}

func (m *Manager) installStatusPath() string {
	return filepath.Join(m.dataDir, "update-install-status")
}

func (m *Manager) previousInstallError() error {
	message, err := m.consumeInstallStatus()
	if err != nil {
		return err
	}
	if message != "" {
		return fmt.Errorf("previous update failed: %s", message)
	}
	return nil
}

func (m *Manager) consumeInstallStatus() (string, error) {
	path := m.installStatusPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read previous update status: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "pending" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", fmt.Errorf("inspect previous update status: %w", statErr)
		}
		if m.now().Before(info.ModTime().Add(2 * time.Minute)) {
			return "", nil
		}
		value = "error: the Windows installer did not report completion"
	}
	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return "", fmt.Errorf("remove previous update status: %w", removeErr)
	}
	if value == "ok" || value == "" {
		return "", nil
	}
	if message, found := strings.CutPrefix(value, "error:"); found {
		return strings.TrimSpace(message), nil
	}
	return "unrecognized installer status", nil
}

func (m *Manager) writeState(value state) error {
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		return fmt.Errorf("create update state directory: %w", err)
	}
	if err := os.Chmod(m.dataDir, 0o700); err != nil {
		return fmt.Errorf("protect update state directory: %w", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(m.dataDir, ".update-state-")
	if err != nil {
		return fmt.Errorf("create update state: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, m.statePath()); err != nil {
		return fmt.Errorf("save update state: %w", err)
	}
	return nil
}

func RunCommand(ctx context.Context, manager *Manager, args []string, output io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "update" {
		return false, nil
	}
	if len(args) == 2 && (args[1] == "help" || args[1] == "--help" || args[1] == "-h") {
		printUsage(output)
		return true, nil
	}
	if len(args) == 1 {
		result, err := manager.Install(ctx)
		if err != nil {
			return true, err
		}
		if !result.Available {
			fmt.Fprintf(output, "lefalock %s is already up to date\n", result.Current)
		} else if result.Deferred {
			fmt.Fprintf(output, "lefalock %s downloaded; installation will finish after this process exits\n", result.Latest)
		} else {
			fmt.Fprintf(output, "lefalock updated from %s to %s\n", result.Current, result.Latest)
		}
		return true, nil
	}
	switch args[1] {
	case "check":
		if len(args) != 2 {
			return true, errors.New("usage: lefalock update check")
		}
		result, err := manager.Check(ctx)
		if err != nil {
			return true, err
		}
		fmt.Fprintf(output, "current version: %s\nlatest version: %s\n", result.Current, result.Latest)
		if result.Available {
			fmt.Fprintln(output, "update available; run lefalock update")
		} else {
			fmt.Fprintln(output, "lefalock is up to date")
		}
		return true, nil
	default:
		return true, fmt.Errorf("unknown update command %q", args[1])
	}
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  lefalock update")
	fmt.Fprintln(output, "  lefalock update check")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  update         Download, verify, and install the latest release")
	fmt.Fprintln(output, "  update check   Check whether a newer release is available")
}
