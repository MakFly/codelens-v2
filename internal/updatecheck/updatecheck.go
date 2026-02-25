package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRepo    = "MakFly/codelens-v2"
	defaultAPIBase = "https://api.github.com"
	defaultTTL     = 12 * time.Hour
)

type Result struct {
	CurrentVersion string
	LatestTag      string
	NeedsUpdate    bool
}

type Checker struct {
	Repo       string
	CachePath  string
	TTL        time.Duration
	Disabled   bool
	APIBaseURL string
	HTTPClient *http.Client
	Now        func() time.Time
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

type cacheRecord struct {
	CheckedAt time.Time `json:"checked_at"`
	LatestTag string    `json:"latest_tag"`
}

func NewDefault() *Checker {
	ttl := defaultTTL
	if raw := strings.TrimSpace(os.Getenv("CODELENS_UPDATE_CHECK_TTL_HOURS")); raw != "" {
		if h, err := strconv.Atoi(raw); err == nil && h > 0 {
			ttl = time.Duration(h) * time.Hour
		}
	}

	return &Checker{
		Repo:       defaultRepo,
		CachePath:  defaultCachePath(),
		TTL:        ttl,
		Disabled:   envTrue("CODELENS_DISABLE_UPDATE_CHECK"),
		APIBaseURL: defaultAPIBase,
		HTTPClient: &http.Client{Timeout: 1500 * time.Millisecond},
		Now:        time.Now,
	}
}

func (c *Checker) Check(ctx context.Context, currentVersion string) (Result, error) {
	currentVersion = strings.TrimSpace(currentVersion)
	res := Result{CurrentVersion: currentVersion}
	if c == nil || c.Disabled || currentVersion == "" || currentVersion == "dev" {
		return res, nil
	}

	latestTag, err := c.readLatestTagFromCache()
	if err != nil || latestTag == "" {
		latestTag, err = c.fetchLatestTag(ctx)
		if err != nil {
			return res, err
		}
		_ = c.writeLatestTagToCache(latestTag)
	}

	res.LatestTag = latestTag
	res.NeedsUpdate = isVersionNewer(currentVersion, latestTag)
	return res, nil
}

func (c *Checker) fetchLatestTag(ctx context.Context) (string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(c.APIBaseURL), "/")
	if base == "" {
		base = defaultAPIBase
	}
	repo := strings.TrimSpace(c.Repo)
	if repo == "" {
		repo = defaultRepo
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", base, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "codelens-cli")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 1500 * time.Millisecond}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases/latest status %d", resp.StatusCode)
	}

	var payload githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return "", fmt.Errorf("empty tag_name from releases/latest")
	}
	return tag, nil
}

func (c *Checker) readLatestTagFromCache() (string, error) {
	if c.CachePath == "" || c.TTL <= 0 {
		return "", fmt.Errorf("cache disabled")
	}
	b, err := os.ReadFile(c.CachePath)
	if err != nil {
		return "", err
	}
	var rec cacheRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return "", err
	}
	now := c.now()
	if rec.CheckedAt.IsZero() || now.Sub(rec.CheckedAt) > c.TTL {
		return "", fmt.Errorf("cache stale")
	}
	tag := strings.TrimSpace(rec.LatestTag)
	if tag == "" {
		return "", fmt.Errorf("cache missing latest tag")
	}
	return tag, nil
}

func (c *Checker) writeLatestTagToCache(tag string) error {
	if c.CachePath == "" || c.TTL <= 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.CachePath), 0o755); err != nil {
		return err
	}
	rec := cacheRecord{
		CheckedAt: c.now().UTC(),
		LatestTag: strings.TrimSpace(tag),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return os.WriteFile(c.CachePath, b, 0o644)
}

func defaultCachePath() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return ".codelens-update-check.json"
		}
		cacheDir = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheDir, "codelens", "update-check.json")
}

func (c *Checker) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func envTrue(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func isVersionNewer(current, latestTag string) bool {
	cur, okCur := parseSemver(current)
	lat, okLat := parseSemver(latestTag)
	if !okCur || !okLat {
		return false
	}

	if lat[0] != cur[0] {
		return lat[0] > cur[0]
	}
	if lat[1] != cur[1] {
		return lat[1] > cur[1]
	}
	return lat[2] > cur[2]
}

func parseSemver(raw string) ([3]int, bool) {
	out := [3]int{}
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "+- "); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return out, false
	}

	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return out, false
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}

	for i := range 3 {
		if i >= len(parts) {
			out[i] = 0
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
