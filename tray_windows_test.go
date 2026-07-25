//go:build windows

package main

import "testing"

func TestTrayUpdateMenuItem(t *testing.T) {
	tests := []struct {
		name     string
		status   UpdateStatus
		wantID   int
		wantText string
		disabled bool
		ok       bool
	}{
		{
			name:     "installing",
			status:   UpdateStatus{Installing: true, PreparedVersion: "0.2.1"},
			wantText: "正在更新…",
			disabled: true,
			ok:       true,
		},
		{
			name:     "downloading",
			status:   UpdateStatus{Downloading: true},
			wantText: "正在下载更新…",
			disabled: true,
			ok:       true,
		},
		{
			name:     "prepared",
			status:   UpdateStatus{PreparedVersion: "0.2.1", Latest: &UpdateInfo{Available: true}},
			wantID:   menuApplyUpdate,
			wantText: "立即更新",
			ok:       true,
		},
		{
			name:     "available",
			status:   UpdateStatus{Latest: &UpdateInfo{Available: true}},
			wantID:   menuDownloadUpdate,
			wantText: "下载更新",
			ok:       true,
		},
		{
			name:   "current",
			status: UpdateStatus{Latest: &UpdateInfo{Available: false}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item, ok := trayUpdateMenuItem(test.status)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if !ok {
				return
			}
			if item.ID != test.wantID || item.Label != test.wantText || item.Disabled != test.disabled {
				t.Fatalf("item = %#v, want id=%d label=%q disabled=%v", item, test.wantID, test.wantText, test.disabled)
			}
		})
	}
}
