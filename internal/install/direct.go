package install

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
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Default release locations. The URL for a file is
// DownloadBase/<repo>/releases/download/<tag>/<file>, and the latest release
// is APIBase/<repo>/releases/latest; HALOTRCER_INSTALL_DOWNLOAD_BASE and
// HALOTRCER_INSTALL_API_BASE replace the bases (mirrors and tests), https or
// loopback http only, as the tools' own installers allow.
const (
	DefaultDownloadBase = "https://github.com/rajasatyajit"
	DefaultAPIBase      = "https://api.github.com/repos/rajasatyajit"
)

// Limits on what a download may be.
const (
	maxArchive   = 256 << 20
	maxSmallFile = 1 << 20
	maxBinary    = 200 << 20
)

// CheckBase validates a mirror base URL.
func CheckBase(name, base string) error {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s: not a URL", name)
	}
	if u.Scheme == "https" {
		return nil
	}
	h := u.Hostname()
	if u.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1") {
		return nil
	}
	return fmt.Errorf("%s must be https (or http to 127.0.0.1, localhost or [::1])", name)
}

var tagRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// NormalizeVersion adds the leading v and checks the shape the tools'
// installers accept.
func NormalizeVersion(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !tagRE.MatchString(v) {
		return "", fmt.Errorf("version %q is not vMAJOR.MINOR.PATCH[-PRE]", v)
	}
	return v, nil
}

// Downloader fetches releases.
type Downloader struct {
	Client       *http.Client
	DownloadBase string
	APIBase      string
}

// NewDownloader uses the environment overrides when set.
func NewDownloader(getenv func(string) string) (*Downloader, error) {
	d := &Downloader{
		Client:       &http.Client{Timeout: 5 * time.Minute},
		DownloadBase: DefaultDownloadBase,
		APIBase:      DefaultAPIBase,
	}
	if v := getenv("HALOTRCER_INSTALL_DOWNLOAD_BASE"); v != "" {
		if err := CheckBase("HALOTRCER_INSTALL_DOWNLOAD_BASE", v); err != nil {
			return nil, err
		}
		d.DownloadBase = strings.TrimRight(v, "/")
	}
	if v := getenv("HALOTRCER_INSTALL_API_BASE"); v != "" {
		if err := CheckBase("HALOTRCER_INSTALL_API_BASE", v); err != nil {
			return nil, err
		}
		d.APIBase = strings.TrimRight(v, "/")
	}
	return d, nil
}

func (d *Downloader) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "halotrcer-install")
	resp, err := d.Client.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf("timed out fetching %s", rawURL)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", rawURL, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", rawURL, limit)
	}
	return b, nil
}

// Latest returns the tag of t's latest stable release (GitHub's latest
// excludes prereleases, as the tools' installers rely on).
func (d *Downloader) Latest(ctx context.Context, t Tool) (string, error) {
	b, err := d.get(ctx, d.APIBase+"/"+t.Repo+"/releases/latest", maxSmallFile)
	if err != nil {
		return "", fmt.Errorf("finding the latest %s release: %w", t.Name, err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &rel); err != nil || !tagRE.MatchString(rel.TagName) {
		return "", fmt.Errorf("finding the latest %s release: no valid tag_name", t.Name)
	}
	return rel.TagName, nil
}

func (d *Downloader) fileURL(t Tool, tag, file string) string {
	return d.DownloadBase + "/" + t.Repo + "/releases/download/" + tag + "/" + file
}

// Platform is the target OS and architecture.
type Platform struct{ OS, Arch string }

// ArchiveName is the release archive for p:
// <name>_<version without v>_<os>_<arch>.tar.gz (.zip on Windows), the
// GoReleaser name_template both tools use.
func ArchiveName(t Tool, tag string, p Platform) string {
	ext := ".tar.gz"
	if p.OS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", t.Name, strings.TrimPrefix(tag, "v"), p.OS, p.Arch, ext)
}

// Verified is a downloaded, verified binary.
type Verified struct {
	Binary    []byte
	Archive   string
	Signature string // "verified", or why it was not checked
}

// Verifier runs cosign; nil means cosign is not installed.
type Verifier func(ctx context.Context, args []string) error

// Fetch downloads t at tag for p and verifies it: the cosign signature over
// checksums.txt first (when cosign is present; required with
// requireCosign), then the archive's SHA-256 against its exact line in
// checksums.txt. Only then is the binary taken out of the archive.
func (d *Downloader) Fetch(ctx context.Context, t Tool, tag string, p Platform, cosign Verifier, requireCosign bool, workDir string) (*Verified, error) {
	archive := ArchiveName(t, tag, p)
	sums, err := d.get(ctx, d.fileURL(t, tag, "checksums.txt"), maxSmallFile)
	if err != nil {
		return nil, err
	}
	v := &Verified{Archive: archive}
	switch {
	case cosign == nil && requireCosign:
		return nil, errors.New("--require-cosign: cosign is not installed (https://docs.sigstore.dev/cosign/system_config/installation/)")
	case cosign == nil:
		v.Signature = "not checked: cosign is not installed (the SHA-256 is still checked)"
	default:
		bundle, err := d.get(ctx, d.fileURL(t, tag, "checksums.txt.sigstore.json"), maxSmallFile)
		if err != nil {
			return nil, fmt.Errorf("signature bundle: %w", err)
		}
		sumsPath := filepath.Join(workDir, "checksums.txt")
		bundlePath := filepath.Join(workDir, "checksums.txt.sigstore.json")
		if err := os.WriteFile(sumsPath, sums, 0o600); err != nil {
			return nil, err
		}
		if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
			return nil, err
		}
		args := []string{"verify-blob", "--bundle", bundlePath,
			"--certificate-identity-regexp", t.CosignIdentity(),
			"--certificate-oidc-issuer", CosignIssuer, sumsPath}
		if err := cosign(ctx, args); err != nil {
			return nil, fmt.Errorf("the signature on %s %s's checksums.txt did not verify: %w", t.Name, tag, err)
		}
		v.Signature = "verified"
	}
	want, err := checksumFor(sums, archive)
	if err != nil {
		return nil, err
	}
	data, err := d.get(ctx, d.fileURL(t, tag, archive), maxArchive)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s: SHA-256 does not match checksums.txt; refusing to install", archive)
	}
	bin := t.Name
	if p.OS == "windows" {
		bin += ".exe"
	}
	if v.Binary, err = extract(data, archive, bin); err != nil {
		return nil, err
	}
	return v, nil
}

// checksumFor finds archive's exact line ("<sha256>  <name>").
func checksumFor(sums []byte, archive string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == archive && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err == nil {
				return strings.ToLower(f[0]), nil
			}
		}
	}
	return "", fmt.Errorf("checksums.txt has no line for %s", archive)
}

// extract takes exactly one regular file, name, from the archive's root.
func extract(data []byte, archive, name string) ([]byte, error) {
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", archive, err)
		}
		for _, f := range zr.File {
			if f.Name != name || !f.Mode().IsRegular() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return readCapped(rc, archive)
		}
		return nil, fmt.Errorf("%s has no %s", archive, name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", archive, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s has no %s", archive, name)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", archive, err)
		}
		if h.Name == name && h.Typeflag == tar.TypeReg {
			return readCapped(tr, archive)
		}
	}
}

func readCapped(r io.Reader, archive string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBinary+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBinary {
		return nil, fmt.Errorf("%s: binary larger than %d bytes", archive, maxBinary)
	}
	return b, nil
}

// Place writes the binary into dir atomically (a temp file in dir, then a
// rename over any existing file), executable.
func Place(dir, name string, bin []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, "."+name+".new-*")
	if err != nil {
		return "", fmt.Errorf("%s is not writable (Halotrcer never uses sudo; choose another --dir): %w", dir, err)
	}
	tn := tmp.Name()
	defer func() { _ = os.Remove(tn) }()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tn, dst); err != nil {
		return "", err
	}
	return dst, nil
}
