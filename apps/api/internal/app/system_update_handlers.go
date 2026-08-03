package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	BuildVersion = "dev"
	BuildCommit  = ""
	BuildDate    = ""
)

type systemVersionInfo struct {
	CurrentVersion  string     `json:"currentVersion"`
	CurrentCommit   string     `json:"currentCommit,omitempty"`
	BuildDate       string     `json:"buildDate,omitempty"`
	LatestVersion   string     `json:"latestVersion,omitempty"`
	LatestName      string     `json:"latestName,omitempty"`
	ReleaseURL      string     `json:"releaseUrl,omitempty"`
	ReleaseNotes    string     `json:"releaseNotes,omitempty"`
	PublishedAt     *time.Time `json:"publishedAt,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	UpdateEnabled   bool       `json:"updateEnabled"`
	CheckError      string     `json:"checkError,omitempty"`
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

func (a *App) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	info, err := a.systemVersion(r.Context())
	if err != nil {
		info.CheckError = "暂时无法连接版本服务"
		a.log.Warn("check system version", "error", err)
	}
	respondJSON(w, http.StatusOK, info)
}

func (a *App) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil || user.Role != "admin" {
		respondError(w, http.StatusForbidden, "system administrator required")
		return
	}
	if !a.updateEnabled() {
		respondError(w, http.StatusServiceUnavailable, "online update is not configured")
		return
	}

	info, err := a.systemVersion(r.Context())
	if err != nil {
		respondError(w, http.StatusBadGateway, "failed to check latest release")
		return
	}
	if !info.UpdateAvailable {
		respondError(w, http.StatusConflict, "already on the latest version")
		return
	}

	backupPath, err := a.backupDatabaseBeforeUpdate(r.Context())
	if err != nil {
		a.log.Error("backup database before update", "error", err)
		respondError(w, http.StatusInternalServerError, "failed to back up database")
		return
	}
	if err := a.triggerUpdateService(r.Context()); err != nil {
		a.log.Error("trigger system update", "error", err)
		respondError(w, http.StatusBadGateway, "failed to start update")
		return
	}

	a.log.Info("system update requested", "user", user.ID, "from", info.CurrentVersion, "to", info.LatestVersion, "backup", backupPath)
	respondJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"currentVersion": info.CurrentVersion,
		"targetVersion":  info.LatestVersion,
		"message":        "更新已启动，服务会在完成后自动恢复",
	})
}

func (a *App) systemVersion(ctx context.Context) (systemVersionInfo, error) {
	current := strings.TrimSpace(a.config().AppVersion)
	if current == "" {
		current = BuildVersion
	}
	info := systemVersionInfo{
		CurrentVersion: current,
		CurrentCommit:  strings.TrimSpace(BuildCommit),
		BuildDate:      strings.TrimSpace(BuildDate),
		UpdateEnabled:  a.updateEnabled(),
	}

	release, err := a.fetchLatestRelease(ctx)
	if err != nil {
		return info, err
	}
	info.LatestVersion = strings.TrimSpace(release.TagName)
	info.LatestName = strings.TrimSpace(release.Name)
	info.ReleaseURL = strings.TrimSpace(release.HTMLURL)
	info.ReleaseNotes = strings.TrimSpace(release.Body)
	if !release.PublishedAt.IsZero() {
		info.PublishedAt = &release.PublishedAt
	}
	info.UpdateAvailable = versionIsNewer(info.LatestVersion, info.CurrentVersion)
	return info, nil
}

func (a *App) fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	endpoint := strings.TrimSpace(a.config().ReleaseAPIURL)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return githubRelease{}, errors.New("invalid release API URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NewSzxcn-Email/"+strings.TrimPrefix(a.config().AppVersion, "v"))
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return githubRelease{}, fmt.Errorf("release API returned %s", resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("release API returned an empty tag")
	}
	return release, nil
}

func (a *App) updateEnabled() bool {
	return strings.TrimSpace(a.config().UpdateServiceURL) != "" && strings.TrimSpace(a.config().UpdateServiceToken) != ""
}

func (a *App) triggerUpdateService(ctx context.Context) error {
	parsed, err := url.Parse(strings.TrimSpace(a.config().UpdateServiceURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("invalid update service URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(a.config().UpdateServiceToken))
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("update service returned %s", resp.Status)
	}
	return nil
}

func (a *App) backupDatabaseBeforeUpdate(ctx context.Context) (string, error) {
	backupDir := filepath.Join(a.config().DataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	backupPath := filepath.Join(backupDir, "pre-update-"+a.now().UTC().Format("20060102T150405.000000000Z")+".db")
	quotedPath := strings.ReplaceAll(backupPath, "'", "''")
	if _, err := a.db.ExecContext(ctx, "VACUUM INTO '"+quotedPath+"'"); err != nil {
		return "", err
	}
	if err := pruneUpdateBackups(backupDir, 5); err != nil {
		a.log.Warn("prune update backups", "error", err)
	}
	return backupPath, nil
}

func pruneUpdateBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type backupFile struct {
		path    string
		modTime time.Time
	}
	backups := make([]backupFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "pre-update-") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, backupFile{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].modTime.After(backups[j].modTime) })
	if keep < 0 {
		keep = 0
	}
	if len(backups) <= keep {
		return nil
	}
	for _, backup := range backups[keep:] {
		if err := os.Remove(backup.path); err != nil {
			return err
		}
	}
	return nil
}

var versionPattern = regexp.MustCompile(`^[vV]?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func versionIsNewer(latest, current string) bool {
	latestParts, latestPrerelease, latestOK := parseVersion(latest)
	currentParts, currentPrerelease, currentOK := parseVersion(current)
	if !latestOK {
		return false
	}
	if !currentOK {
		return true
	}
	for i := 0; i < len(latestParts); i++ {
		if latestParts[i] != currentParts[i] {
			return latestParts[i] > currentParts[i]
		}
	}
	return currentPrerelease != "" && latestPrerelease == ""
}

func parseVersion(value string) ([3]int, string, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return [3]int{}, "", false
	}
	var parts [3]int
	for i := 0; i < 3; i++ {
		if match[i+1] == "" {
			continue
		}
		part, err := strconv.Atoi(match[i+1])
		if err != nil {
			return [3]int{}, "", false
		}
		parts[i] = part
	}
	return parts, match[4], true
}
