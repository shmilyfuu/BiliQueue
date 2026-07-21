package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	updateInitialDelay = 10 * time.Second
	updateCheckPeriod  = 24 * time.Hour
	maxUpdateBytes     = 200 << 20
	updateWorkspaceDir = ".biliqueue-update"
)

var (
	giteeLatestReleaseURL  = "https://gitee.com/api/v5/repos/shmilyfuu/BiliQueue/releases/latest"
	githubLatestReleaseURL = "https://api.github.com/repos/shmilyfuu/BiliQueue/releases/latest"
	versionPattern         = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9a-z.-]+))?$`)
	releaseNoteHeading     = regexp.MustCompile(`(?im)^#{1,6}\s+BiliQueue\s+v([0-9][0-9a-z.-]*)\s*$`)
	updateExecutablePath   = os.Executable
)

//go:embed RELEASE_NOTES.md
var releaseNotesMarkdown string

type UpdateInfo struct {
	Available   bool   `json:"available"`
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	Source      string `json:"source"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	PageURL     string `json:"pageUrl,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ChecksumURL string `json:"-"`
}

type UpdateStatus struct {
	Checking        bool        `json:"checking"`
	Downloading     bool        `json:"downloading"`
	Installing      bool        `json:"installing"`
	LastCheckedAt   int64       `json:"lastCheckedAt,omitempty"`
	Error           string      `json:"error,omitempty"`
	Latest          *UpdateInfo `json:"latest,omitempty"`
	PreparedVersion string      `json:"preparedVersion,omitempty"`
	Deferred        bool        `json:"deferred,omitempty"`
}

type embeddedReleaseNote struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type releaseResponse struct {
	ID      int            `json:"id"`
	TagName string         `json:"tag_name"`
	Name    string         `json:"name"`
	Body    string         `json:"body"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

type updateSource struct {
	Name           string
	LatestURL      string
	AttachmentsURL string
	PageBase       string
}

type preparedUpdate struct {
	Root        string
	PackageRoot string
	HelperEXE   string
	Version     string
}

type deferredUpdateMarker struct {
	Version string `json:"version"`
}

func latestEmbeddedReleaseNotes() string {
	notes := strings.TrimSpace(strings.ReplaceAll(releaseNotesMarkdown, "\r\n", "\n"))
	if index := strings.Index(notes, "\n---\n"); index >= 0 {
		notes = strings.TrimSpace(notes[:index])
	}
	return notes
}

func embeddedReleaseNotes() []embeddedReleaseNote {
	notes := strings.TrimSpace(strings.ReplaceAll(releaseNotesMarkdown, "\r\n", "\n"))
	sections := strings.Split(notes, "\n---\n")
	releases := make([]embeddedReleaseNote, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		match := releaseNoteHeading.FindStringSubmatchIndex(section)
		if match == nil {
			continue
		}
		releases = append(releases, embeddedReleaseNote{
			Version: section[match[2]:match[3]],
			Notes:   strings.TrimSpace(section[match[1]:]),
		})
	}
	return releases
}

func parseVersion(value string) ([3]int, string, bool) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return [3]int{}, "", false
	}
	var numbers [3]int
	for i := range numbers {
		numbers[i], _ = strconv.Atoi(match[i+1])
	}
	return numbers, strings.ToLower(match[4]), true
}

func compareVersions(left, right string) int {
	l, lPre, lOK := parseVersion(left)
	r, rPre, rOK := parseVersion(right)
	if !lOK || !rOK {
		return strings.Compare(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	for i := range l {
		if l[i] < r[i] {
			return -1
		}
		if l[i] > r[i] {
			return 1
		}
	}
	if lPre == rPre {
		return 0
	}
	if lPre == "" {
		return 1
	}
	if rPre == "" {
		return -1
	}
	return strings.Compare(lPre, rPre)
}

func fetchRelease(ctx context.Context, client *http.Client, source updateSource) (UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.LatestURL, nil)
	if err != nil {
		return UpdateInfo{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BiliQueue-Updater/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("%s 返回 HTTP %d", source.Name, resp.StatusCode)
	}
	var release releaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return UpdateInfo{}, fmt.Errorf("解析 %s Release：%w", source.Name, err)
	}
	if source.AttachmentsURL != "" {
		attachmentsURL := fmt.Sprintf(source.AttachmentsURL, release.ID)
		attachmentsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, attachmentsURL, nil)
		if err != nil {
			return UpdateInfo{}, err
		}
		attachmentsReq.Header.Set("Accept", "application/json")
		attachmentsReq.Header.Set("User-Agent", "BiliQueue-Updater/"+version)
		attachmentsResp, err := client.Do(attachmentsReq)
		if err != nil {
			return UpdateInfo{}, err
		}
		defer attachmentsResp.Body.Close()
		if attachmentsResp.StatusCode != http.StatusOK {
			return UpdateInfo{}, fmt.Errorf("%s 附件接口返回 HTTP %d", source.Name, attachmentsResp.StatusCode)
		}
		var attachments []releaseAsset
		if err := json.NewDecoder(io.LimitReader(attachmentsResp.Body, 2<<20)).Decode(&attachments); err != nil {
			return UpdateInfo{}, fmt.Errorf("解析 %s Release 附件：%w", source.Name, err)
		}
		release.Assets = append(release.Assets, attachments...)
	}
	remoteVersion := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if _, _, ok := parseVersion(remoteVersion); !ok {
		return UpdateInfo{}, fmt.Errorf("%s Release 版本号无效：%q", source.Name, release.TagName)
	}
	expectedAsset := fmt.Sprintf("BiliQueue-v%s-windows.zip", remoteVersion)
	var downloadURL, digest, checksumURL string
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, expectedAsset) {
			downloadURL = strings.TrimSpace(asset.BrowserDownloadURL)
			digest = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(asset.Digest)), "sha256:")
		}
		if strings.EqualFold(asset.Name, expectedAsset+".sha256") {
			checksumURL = strings.TrimSpace(asset.BrowserDownloadURL)
		}
	}
	if downloadURL == "" {
		return UpdateInfo{}, fmt.Errorf("%s Release 缺少 %s", source.Name, expectedAsset)
	}
	pageURL := strings.TrimSpace(release.HTMLURL)
	if pageURL == "" {
		pageURL = source.PageBase + "/releases/tag/" + release.TagName
	}
	return UpdateInfo{
		Available: compareVersions(version, remoteVersion) < 0,
		Version:   remoteVersion, Tag: release.TagName, Name: release.Name,
		Notes: strings.TrimSpace(release.Body), Source: source.Name,
		DownloadURL: downloadURL, PageURL: pageURL, SHA256: digest, ChecksumURL: checksumURL,
	}, nil
}

func checkLatestRelease(ctx context.Context) (UpdateInfo, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	gitee := updateSource{
		Name:           "Gitee",
		LatestURL:      giteeLatestReleaseURL,
		AttachmentsURL: "https://gitee.com/api/v5/repos/shmilyfuu/BiliQueue/releases/%d/attach_files",
		PageBase:       "https://gitee.com/shmilyfuu/BiliQueue",
	}
	github := updateSource{Name: "GitHub", LatestURL: githubLatestReleaseURL, PageBase: "https://github.com/shmilyfuu/BiliQueue"}
	primary, primaryErr := fetchRelease(ctx, client, gitee)
	if primaryErr == nil && primary.Available {
		return primary, nil
	}
	fallback, fallbackErr := fetchRelease(ctx, client, github)
	if fallbackErr == nil {
		if primaryErr != nil || compareVersions(fallback.Version, primary.Version) > 0 {
			return fallback, nil
		}
		return primary, nil
	}
	if primaryErr == nil {
		return primary, nil
	}
	return UpdateInfo{}, fmt.Errorf("Gitee：%v；GitHub：%v", primaryErr, fallbackErr)
}

func (a *App) checkForUpdates(ctx context.Context) (UpdateInfo, error) {
	a.updateCheckMu.Lock()
	defer a.updateCheckMu.Unlock()
	a.mu.Lock()
	a.updateStatus.Checking = true
	a.updateStatus.Error = ""
	a.mu.Unlock()
	a.broadcast()
	info, err := checkLatestRelease(ctx)
	a.mu.Lock()
	a.updateStatus.Checking = false
	a.updateStatus.LastCheckedAt = time.Now().UnixMilli()
	if err != nil {
		a.updateStatus.Error = err.Error()
	} else {
		a.updateStatus.Latest = &info
		a.updateStatus.Error = ""
	}
	a.mu.Unlock()
	a.broadcast()
	return info, err
}

func (a *App) setAutoCheckUpdates(enabled bool) {
	a.mu.Lock()
	changed := a.config.Updates.AutoCheck != enabled
	a.config.Updates.AutoCheck = enabled
	a.mu.Unlock()
	if changed {
		a.saveConfig()
		a.broadcast()
	}
	if enabled {
		go a.runAutomaticUpdateCheck()
	}
}

func (a *App) startAutoUpdateChecks() {
	go func() {
		timer := time.NewTimer(updateInitialDelay)
		defer timer.Stop()
		for {
			<-timer.C
			a.mu.RLock()
			enabled := a.config.Updates.AutoCheck
			a.mu.RUnlock()
			if enabled {
				a.runAutomaticUpdateCheck()
			}
			timer.Reset(updateCheckPeriod)
		}
	}()
}

func (a *App) runAutomaticUpdateCheck() {
	info, err := a.checkForUpdates(context.Background())
	if err != nil {
		log.Printf("automatic update check: %v", err)
		return
	}
	if !info.Available {
		return
	}
	a.mu.Lock()
	if a.updateNotifiedVersion == info.Version {
		a.mu.Unlock()
		return
	}
	a.updateNotifiedVersion = info.Version
	a.mu.Unlock()
	notifyUpdateAvailable(a, info)
}

func (a *App) latestAvailableUpdate(ctx context.Context) (UpdateInfo, error) {
	a.mu.RLock()
	latest := a.updateStatus.Latest
	a.mu.RUnlock()
	if latest == nil || !latest.Available {
		info, err := a.checkForUpdates(ctx)
		if err != nil {
			return UpdateInfo{}, err
		}
		latest = &info
	}
	if !latest.Available {
		return UpdateInfo{}, errors.New("当前已经是最新版本")
	}
	return *latest, nil
}

func (a *App) downloadLatestUpdate(ctx context.Context) (UpdateInfo, error) {
	a.updateInstallMu.Lock()
	defer a.updateInstallMu.Unlock()
	latest, err := a.latestAvailableUpdate(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	if a.preparedUpdate != nil && a.preparedUpdate.Version == latest.Version {
		if _, err := os.Stat(a.preparedUpdate.HelperEXE); err == nil {
			a.mu.Lock()
			a.updateStatus.PreparedVersion = latest.Version
			a.updateStatus.Error = ""
			a.mu.Unlock()
			a.broadcast()
			return latest, nil
		}
		a.preparedUpdate = nil
	}
	a.mu.Lock()
	a.updateStatus.Downloading = true
	a.updateStatus.Error = ""
	a.mu.Unlock()
	a.broadcast()
	prepared, err := a.downloadAndPrepareUpdate(ctx, latest)
	a.mu.Lock()
	a.updateStatus.Downloading = false
	if err != nil {
		a.updateStatus.Error = err.Error()
		a.mu.Unlock()
		a.broadcast()
		return UpdateInfo{}, err
	}
	a.preparedUpdate = &prepared
	a.updateStatus.PreparedVersion = latest.Version
	a.updateStatus.Deferred = false
	a.updateStatus.Error = ""
	a.mu.Unlock()
	a.broadcast()
	return latest, nil
}

func (a *App) applyPreparedUpdate() error {
	a.updateInstallMu.Lock()
	defer a.updateInstallMu.Unlock()
	if a.preparedUpdate == nil {
		return errors.New("尚未下载并准备更新包")
	}
	prepared := *a.preparedUpdate
	a.mu.Lock()
	a.updateStatus.Installing = true
	a.updateStatus.Error = ""
	a.mu.Unlock()
	a.broadcast()
	if err := launchUpdateHelper(a, prepared); err != nil {
		a.mu.Lock()
		a.updateStatus.Installing = false
		a.updateStatus.Error = err.Error()
		a.mu.Unlock()
		a.broadcast()
		return err
	}
	return nil

}

func (a *App) deferPreparedUpdate() (string, error) {
	a.updateInstallMu.Lock()
	defer a.updateInstallMu.Unlock()
	if a.preparedUpdate == nil {
		return "", errors.New("尚未下载并准备更新包")
	}
	prepared := *a.preparedUpdate
	if err := saveDeferredUpdate(prepared); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.updateStatus.Deferred = true
	a.updateStatus.PreparedVersion = prepared.Version
	a.updateStatus.Error = ""
	a.mu.Unlock()
	a.broadcast()
	return prepared.Version, nil
}

func (a *App) installLatestUpdate(ctx context.Context) error {
	if _, err := a.downloadLatestUpdate(ctx); err != nil {
		return err
	}
	return a.applyPreparedUpdate()
}

func updateWorkspaceRoot() (string, error) {
	executable, err := updateExecutablePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), updateWorkspaceDir), nil
}

func deferredUpdatePath() (string, error) {
	root, err := updateWorkspaceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "pending.json"), nil
}

func saveDeferredUpdate(prepared preparedUpdate) error {
	if _, _, ok := parseVersion(prepared.Version); !ok {
		return errors.New("待安装更新版本号无效")
	}
	path, err := deferredUpdatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeJSONAtomic(path, deferredUpdateMarker{Version: prepared.Version})
}

func loadDeferredUpdate() (preparedUpdate, bool, error) {
	path, err := deferredUpdatePath()
	if err != nil {
		return preparedUpdate{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return preparedUpdate{}, false, nil
	}
	if err != nil {
		return preparedUpdate{}, false, err
	}
	var marker deferredUpdateMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return preparedUpdate{}, true, fmt.Errorf("读取待安装更新：%w", err)
	}
	if _, _, ok := parseVersion(marker.Version); !ok {
		return preparedUpdate{}, true, errors.New("待安装更新版本号无效")
	}
	workspace, err := updateWorkspaceRoot()
	if err != nil {
		return preparedUpdate{}, true, err
	}
	root := filepath.Join(workspace, "v"+marker.Version)
	helper, err := findUpdateExecutable(filepath.Join(root, "package"))
	if err != nil {
		return preparedUpdate{}, true, fmt.Errorf("待安装更新包无效：%w", err)
	}
	return preparedUpdate{Root: root, PackageRoot: filepath.Dir(helper), HelperEXE: helper, Version: marker.Version}, true, nil
}

func discardDeferredUpdate() {
	path, err := deferredUpdatePath()
	if err == nil {
		_ = os.Remove(path)
	}
}

func (a *App) downloadAndPrepareUpdate(ctx context.Context, info UpdateInfo) (preparedUpdate, error) {
	if err := validateSelfUpdateTarget(); err != nil {
		return preparedUpdate{}, err
	}
	workspace, err := updateWorkspaceRoot()
	if err != nil {
		return preparedUpdate{}, err
	}
	root := filepath.Join(workspace, "v"+info.Version)
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return preparedUpdate{}, err
	}
	zipPath := filepath.Join(root, "package.zip")
	expectedSHA := info.SHA256
	if expectedSHA == "" && info.ChecksumURL != "" {
		expectedSHA, err = fetchUpdateChecksum(ctx, info.ChecksumURL)
		if err != nil {
			_ = os.RemoveAll(root)
			return preparedUpdate{}, fmt.Errorf("读取更新包校验文件：%w", err)
		}
	}
	if err := downloadUpdateFile(ctx, info.DownloadURL, zipPath, expectedSHA); err != nil {
		_ = os.RemoveAll(root)
		return preparedUpdate{}, err
	}
	extractRoot := filepath.Join(root, "package")
	if err := extractUpdateZip(zipPath, extractRoot); err != nil {
		_ = os.RemoveAll(root)
		return preparedUpdate{}, err
	}
	helper, err := findUpdateExecutable(extractRoot)
	if err != nil {
		_ = os.RemoveAll(root)
		return preparedUpdate{}, err
	}
	return preparedUpdate{Root: root, PackageRoot: filepath.Dir(helper), HelperEXE: helper, Version: info.Version}, nil
}

func fetchUpdateChecksum(ctx context.Context, rawURL string) (string, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BiliQueue-Updater/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("校验文件返回 HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("更新包校验文件格式无效")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("更新包校验值无效")
	}
	return strings.ToLower(fields[0]), nil
}

func downloadUpdateFile(ctx context.Context, rawURL, target, expectedSHA string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BiliQueue-Updater/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载更新包：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新包返回 HTTP %d", resp.StatusCode)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxUpdateBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxUpdateBytes {
		return errors.New("更新包超过允许的大小")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("更新包 SHA-256 校验失败：期望 %s，实际 %s", expectedSHA, actual)
	}
	if expectedSHA == "" {
		log.Printf("update package from %s has no published SHA-256; downloaded digest=%s", rawURL, actual)
	}
	return nil
}

func extractUpdateZip(zipPath, destination string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开更新包：%w", err)
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	base, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	var total int64
	for _, item := range reader.File {
		cleanName := filepath.Clean(filepath.FromSlash(item.Name))
		if cleanName == "." || filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("更新包包含不安全路径：%s", item.Name)
		}
		target := filepath.Join(base, cleanName)
		if target != base && !strings.HasPrefix(target, base+string(filepath.Separator)) {
			return fmt.Errorf("更新包路径越界：%s", item.Name)
		}
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		total += int64(item.UncompressedSize64)
		if total > maxUpdateBytes {
			return errors.New("解压后的更新包超过允许的大小")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := item.Open()
		if err != nil {
			return err
		}
		destinationFile, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(destinationFile, source)
		closeSourceErr := source.Close()
		closeDestinationErr := destinationFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeSourceErr != nil {
			return closeSourceErr
		}
		if closeDestinationErr != nil {
			return closeDestinationErr
		}
	}
	return nil
}

func findUpdateExecutable(root string) (string, error) {
	var result string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "BiliQueue-windows-amd64.exe") {
			return nil
		}
		if result != "" {
			return errors.New("更新包中包含多个主程序")
		}
		result = path
		return nil
	})
	if err != nil {
		return "", err
	}
	if result == "" {
		return "", errors.New("更新包中未找到 BiliQueue-windows-amd64.exe")
	}
	return result, nil
}
