package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SourceType classifies the origin of a watch source.
type SourceType string

const (
	SourceLocal SourceType = "local"
	SourceURL   SourceType = "url"
	SourceGit   SourceType = "git"
)

var (
	gitDomains = []string{"github.com", "gitlab.com", "bitbucket.org"}
	gitSSHRe   = regexp.MustCompile(`^git@([^:]+):(.+)$`)
)

// DetectSourceType determines the type of a watch source path.
func DetectSourceType(path string) SourceType {
	if IsGitRepoURL(path) {
		return SourceGit
	}
	if IsURL(path) {
		return SourceURL
	}
	return SourceLocal
}

// IsURL checks if a string is an HTTP(S) URL.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// IsGitRepoURL checks if a URL points to a cloneable git repository.
func IsGitRepoURL(s string) bool {
	if strings.HasPrefix(s, "git@") {
		return gitSSHRe.MatchString(s)
	}
	if strings.HasPrefix(s, "ssh://") || strings.HasPrefix(s, "git://") {
		return true
	}
	if !IsURL(s) {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if !isGitDomain(u.Host) {
		return false
	}
	parts := cleanPathParts(u.Path)
	if len(parts) < 2 {
		return false
	}
	// Exactly org/repo
	if len(parts) == 2 {
		return true
	}
	// org/repo.git
	if len(parts) == 2 && strings.HasSuffix(parts[1], ".git") {
		return true
	}
	// org/repo/tree/<branch> is also a valid repo ref
	if len(parts) == 4 && parts[2] == "tree" {
		return true
	}
	return false
}

// ParseGitRepoPath extracts org/repo from a git URL.
func ParseGitRepoPath(rawURL string) string {
	if m := gitSSHRe.FindStringSubmatch(rawURL); m != nil {
		pathPart := m[2]
		pathPart = strings.TrimSuffix(pathPart, ".git")
		parts := strings.SplitN(pathPart, "/", 3)
		if len(parts) >= 2 {
			return sanitize(parts[0]) + "/" + sanitize(parts[1])
		}
		return sanitize(pathPart)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return hashFilename(rawURL)
	}
	parts := cleanPathParts(u.Path)
	if len(parts) >= 2 {
		repo := strings.TrimSuffix(parts[1], ".git")
		return sanitize(parts[0]) + "/" + sanitize(repo)
	}
	return hashFilename(rawURL)
}

// DownloadURL fetches a URL and saves it to a local file, returning the path.
func DownloadURL(rawURL, destDir string, timeout time.Duration) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Viking-Go/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}

	filename := filenameFromURL(rawURL, resp.Header.Get("Content-Type"))
	destPath := filepath.Join(destDir, filename)

	f, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("write %s: %w", destPath, err)
	}

	return destPath, nil
}

// GitClone clones a git repository into the given directory.
// If the directory already exists, it performs a git pull instead.
func GitClone(repoURL, destDir string) error {
	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		return gitPull(destDir)
	}

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	cmd := exec.Command("git", "clone", "--depth", "1", repoURL, destDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %s (%w)", repoURL, string(out), err)
	}
	return nil
}

func gitPull(dir string) error {
	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull in %s: %s (%w)", dir, string(out), err)
	}
	return nil
}

func isGitDomain(host string) bool {
	for _, d := range gitDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func cleanPathParts(p string) []string {
	var parts []string
	for _, s := range strings.Split(p, "/") {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func hashFilename(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func filenameFromURL(rawURL, contentType string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return hashFilename(rawURL) + ".bin"
	}

	base := filepath.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		base = hashFilename(rawURL)
	}

	if filepath.Ext(base) != "" {
		return base
	}

	ext := extFromContentType(contentType)
	return base + ext
}

func extFromContentType(ct string) string {
	ct = strings.ToLower(ct)
	switch {
	case strings.Contains(ct, "html"):
		return ".html"
	case strings.Contains(ct, "json"):
		return ".json"
	case strings.Contains(ct, "pdf"):
		return ".pdf"
	case strings.Contains(ct, "text"):
		return ".txt"
	case strings.Contains(ct, "xml"):
		return ".xml"
	default:
		return ".bin"
	}
}
