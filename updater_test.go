package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"0.1.15", "0.1.16", -1},
		{"0.1.16-test1", "0.1.16", -1},
		{"0.1.16", "0.1.16", 0},
		{"0.2.0", "0.1.99", 1},
	}
	for _, test := range tests {
		got := compareVersions(test.left, test.right)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != test.want {
			t.Fatalf("compareVersions(%q, %q)=%d want=%d", test.left, test.right, got, test.want)
		}
	}
}

func TestFetchReleaseSelectsWindowsPackage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v9.8.7",
			"name":     "BiliQueue v9.8.7",
			"body":     "最新内容",
			"assets": []map[string]any{
				{"name": "BiliQueue-v9.8.7-source.zip", "browser_download_url": serverURL(r) + "/source"},
				{"name": "BiliQueue-v9.8.7-windows.zip", "browser_download_url": serverURL(r) + "/windows", "digest": "sha256:abcdef"},
			},
		})
	}))
	defer server.Close()
	info, err := fetchRelease(context.Background(), server.Client(), updateSource{Name: "test", LatestURL: server.URL, PageBase: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Version != "9.8.7" || info.DownloadURL != server.URL+"/windows" || info.SHA256 != "abcdef" {
		t.Fatalf("unexpected update info: %#v", info)
	}
}

func TestFetchReleaseLoadsGiteeAttachments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       42,
				"tag_name": "v9.8.7",
				"name":     "BiliQueue v9.8.7",
				"assets": []map[string]any{
					{"name": "v9.8.7.zip", "browser_download_url": serverURL(r) + "/source"},
				},
			})
		case "/releases/42/attach_files":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "BiliQueue-v9.8.7-windows.zip", "browser_download_url": serverURL(r) + "/windows"},
				{"name": "BiliQueue-v9.8.7-windows.zip.sha256", "browser_download_url": serverURL(r) + "/checksum"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	info, err := fetchRelease(context.Background(), server.Client(), updateSource{
		Name:           "Gitee",
		LatestURL:      server.URL + "/latest",
		AttachmentsURL: server.URL + "/releases/%d/attach_files",
		PageBase:       server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.DownloadURL != server.URL+"/windows" || info.ChecksumURL != server.URL+"/checksum" {
		t.Fatalf("unexpected Gitee update info: %#v", info)
	}
}

func TestFetchUpdateChecksumRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-a-sha256"))
	}))
	defer server.Close()

	if _, err := fetchUpdateChecksum(context.Background(), server.URL); err == nil {
		t.Fatal("expected invalid checksum response to be rejected")
	}
}

func TestDownloadUpdateFileReportsProgress(t *testing.T) {
	payload := []byte("progress-payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "package.zip")
	var reports []updateDownloadProgress
	verified := false
	err := downloadUpdateFileWithProgress(context.Background(), server.URL, target, "", func(progress updateDownloadProgress) {
		reports = append(reports, progress)
	}, func() {
		verified = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Fatal("expected at least one progress report")
	}
	last := reports[len(reports)-1]
	if last.DownloadedBytes != int64(len(payload)) || last.TotalBytes != int64(len(payload)) {
		t.Fatalf("unexpected final progress: %#v", last)
	}
	if !verified {
		t.Fatal("expected verification callback")
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, payload) {
		t.Fatalf("downloaded payload=%q want=%q", written, payload)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestExtractUpdateZipRejectsTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "extract")
	if err := extractUpdateZip(zipPath, destination); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestFetchReleaseNotesFiltersAndSorts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "not-a-version", "body": "ignored"},
			{"tag_name": "v1.5.0", "body": "older", "html_url": serverURL(r) + "/v1.5.0"},
			{"tag_name": "v2.0.0-test1", "body": "preview", "prerelease": true},
			{"tag_name": "v2.0.0", "body": "latest", "published_at": "2026-01-02T03:04:05Z"},
			{"tag_name": "v1.0.0", "body": "draft", "draft": true},
		})
	}))
	defer server.Close()

	got, err := fetchReleaseNotes(context.Background(), server.Client(), updateSource{
		Name: "test", ReleasesURL: server.URL, PageBase: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len(fetchReleaseNotes())=%d want=2: %#v", len(got), got)
	}
	if got[0].Version != "2.0.0" || got[0].Notes != "latest" || got[0].Source != "test" {
		t.Fatalf("unexpected latest release: %#v", got[0])
	}
	if got[1].Version != "1.5.0" || got[1].PageURL != server.URL+"/v1.5.0" {
		t.Fatalf("unexpected older release: %#v", got[1])
	}
}

func TestMergeReleaseNotesPrefersGiteeAndFillsHistoryFromGitHub(t *testing.T) {
	gitee := []releaseNote{
		{Version: "2.0.0", Notes: "Gitee latest", Source: "Gitee"},
		{Version: "1.0.0", Notes: "Gitee oldest", Source: "Gitee"},
	}
	github := []releaseNote{
		{Version: "2.0.0", Notes: "GitHub latest", Source: "GitHub"},
		{Version: "1.5.0", Notes: "GitHub middle", Source: "GitHub"},
	}
	got := mergeReleaseNotes(gitee, github)
	if len(got) != 3 {
		t.Fatalf("len(mergeReleaseNotes())=%d want=3: %#v", len(got), got)
	}
	if got[0].Version != "2.0.0" || got[0].Notes != "Gitee latest" || got[0].Source != "Gitee" {
		t.Fatalf("primary source was not preferred: %#v", got[0])
	}
	if got[1].Version != "1.5.0" || got[1].Source != "GitHub" || got[2].Version != "1.0.0" {
		t.Fatalf("fallback history was not merged and sorted: %#v", got)
	}
}

func TestReleaseNotesCacheAvoidsRepeatedFetches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v2.0.0", "body": "notes"}})
	}))
	defer server.Close()

	oldGitee, oldGitHub := giteeReleasesURL, githubReleasesURL
	giteeReleasesURL, githubReleasesURL = server.URL, server.URL
	t.Cleanup(func() {
		giteeReleasesURL, githubReleasesURL = oldGitee, oldGitHub
	})

	var cache releaseNotesCache
	first, err := cache.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached || !second.Cached {
		t.Fatalf("unexpected cache flags: first=%v second=%v", first.Cached, second.Cached)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d want=2 (one request per source on first load)", requests.Load())
	}
}

func TestFetchReleaseNotesCatalogUsesGitHubFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gitee" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v2.0.0", "body": "GitHub fallback"},
		})
	}))
	defer server.Close()

	oldGitee, oldGitHub := giteeReleasesURL, githubReleasesURL
	giteeReleasesURL, githubReleasesURL = server.URL+"/gitee", server.URL+"/github"
	t.Cleanup(func() {
		giteeReleasesURL, githubReleasesURL = oldGitee, oldGitHub
	})

	catalog, err := fetchReleaseNotesCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Releases) != 1 || catalog.Releases[0].Source != "GitHub" {
		t.Fatalf("unexpected fallback catalog: %#v", catalog)
	}
	if len(catalog.Sources) != 1 || catalog.Sources[0] != "GitHub" {
		t.Fatalf("unexpected successful sources: %#v", catalog.Sources)
	}
}

func TestUpdateNotesRouteUsesRemoteReleaseCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v2.0.0", "body": "remote release notes"},
		})
	}))
	defer server.Close()

	oldGitee, oldGitHub := giteeReleasesURL, githubReleasesURL
	giteeReleasesURL, githubReleasesURL = server.URL, server.URL
	t.Cleanup(func() {
		giteeReleasesURL, githubReleasesURL = oldGitee, oldGitHub
	})

	a := newApp(t.TempDir())
	request := httptest.NewRequest(http.MethodGet, "/api/update/notes", nil)
	response := httptest.NewRecorder()
	a.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Version  string        `json:"version"`
		Releases []releaseNote `json:"releases"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != version || len(payload.Releases) != 1 || payload.Releases[0].Notes != "remote release notes" {
		t.Fatalf("unexpected update notes payload: %#v", payload)
	}
}

func TestDeferredUpdateUsesExecutableSiblingWorkspace(t *testing.T) {
	installRoot := t.TempDir()
	runningEXE := filepath.Join(installRoot, "BiliQueue-windows-amd64.exe")
	previous := updateExecutablePath
	updateExecutablePath = func() (string, error) { return runningEXE, nil }
	t.Cleanup(func() { updateExecutablePath = previous })

	workspace, err := updateWorkspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := filepath.Join(installRoot, updateWorkspaceDir)
	if workspace != wantWorkspace {
		t.Fatalf("workspace=%q want=%q", workspace, wantWorkspace)
	}

	version := "9.8.7"
	packageRoot := filepath.Join(workspace, "v"+version, "package", "BiliQueue-v"+version+"-windows")
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(packageRoot, "BiliQueue-windows-amd64.exe")
	if err := os.WriteFile(helper, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepared := preparedUpdate{
		Root: filepath.Join(workspace, "v"+version), PackageRoot: packageRoot,
		HelperEXE: helper, Version: version,
	}
	if err := saveDeferredUpdate(prepared); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := loadDeferredUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !exists || loaded.Version != version || loaded.HelperEXE != helper {
		t.Fatalf("unexpected deferred update: exists=%v update=%#v", exists, loaded)
	}
}

func TestDownloadLatestUpdateReusesPreparedPackage(t *testing.T) {
	a := newApp(t.TempDir())
	helper := filepath.Join(t.TempDir(), "BiliQueue-windows-amd64.exe")
	if err := os.WriteFile(helper, []byte("prepared"), 0o700); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.updateStatus.Latest = &UpdateInfo{Available: true, Version: "9.8.7"}
	a.mu.Unlock()
	a.preparedUpdate = &preparedUpdate{HelperEXE: helper, Version: "9.8.7"}

	info, err := a.downloadLatestUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "9.8.7" || a.updateStatus.PreparedVersion != "9.8.7" {
		t.Fatalf("prepared package was not reused: info=%#v status=%#v", info, a.updateStatus)
	}
}
