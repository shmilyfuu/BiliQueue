package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestLatestEmbeddedReleaseNotesOnlyReturnsFirstSection(t *testing.T) {
	old := releaseNotesMarkdown
	releaseNotesMarkdown = "# v2\n\nlatest\n---\n# v1\n\nold"
	t.Cleanup(func() { releaseNotesMarkdown = old })
	if got := latestEmbeddedReleaseNotes(); got != "# v2\n\nlatest" {
		t.Fatalf("unexpected latest notes: %q", got)
	}
}

func TestEmbeddedReleaseNotesReturnsVersionedHistory(t *testing.T) {
	old := releaseNotesMarkdown
	releaseNotesMarkdown = "# BiliQueue v2.0.0\n\nlatest\n---\n## BiliQueue v1.5.0\n\nold\n---\nnotes without a version"
	t.Cleanup(func() { releaseNotesMarkdown = old })

	got := embeddedReleaseNotes()
	if len(got) != 2 {
		t.Fatalf("len(embeddedReleaseNotes())=%d want=2", len(got))
	}
	if got[0].Version != "2.0.0" || got[0].Notes != "latest" {
		t.Fatalf("unexpected latest release: %#v", got[0])
	}
	if got[1].Version != "1.5.0" || got[1].Notes != "old" {
		t.Fatalf("unexpected previous release: %#v", got[1])
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
