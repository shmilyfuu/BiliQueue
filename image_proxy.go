package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxProxyImageBytes = 8 << 20

func (a *App) imageCacheDir() string {
	return filepath.Join(a.dataDir, "avatars")
}

func allowedBiliImageHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range []string{"hdslb.com", "bilibili.com", "biliimg.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func normalizeProxyImageURL(raw string) (*url.URL, error) {
	raw = normalizeBiliImageURL(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("头像地址格式不正确")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("头像地址协议不受支持")
	}
	if !allowedBiliImageHost(u.Hostname()) {
		return nil, errors.New("头像地址域名不受支持")
	}
	return u, nil
}

func imageCacheName(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]) + ".img"
}

func detectImageContentType(data []byte) string {
	contentType := http.DetectContentType(data)
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	return "application/octet-stream"
}

func (a *App) serveCachedImage(w http.ResponseWriter, path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	w.Header().Set("Content-Type", detectImageContentType(data))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return true
}

func (a *App) fetchAndCacheImage(ctx context.Context, raw string, path string) ([]byte, error) {
	u, err := normalizeProxyImageURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", biliUserAgent)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	a.mu.RLock()
	cookie := a.auth.Cookie
	a.mu.RUnlock()
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("B站图片请求失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProxyImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxProxyImageBytes {
		return nil, errors.New("图片为空或尺寸超出限制")
	}
	if !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return nil, errors.New("返回内容不是图片")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".image-*.tmp")
	if err == nil {
		tmpName := tmp.Name()
		if _, writeErr := tmp.Write(data); writeErr == nil {
			_ = tmp.Chmod(0o600)
		}
		closeErr := tmp.Close()
		if closeErr == nil {
			if runtimeRemoveForRename(path) == nil {
				_ = os.Rename(tmpName, path)
			} else {
				_ = os.Remove(tmpName)
			}
		} else {
			_ = os.Remove(tmpName)
		}
	}
	return data, nil
}

func runtimeRemoveForRename(path string) error {
	// Removing a missing file is harmless. It also makes rename work on Windows.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) handleMediaImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		http.Error(w, "缺少图片地址", http.StatusBadRequest)
		return
	}
	u, err := normalizeProxyImageURL(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	normalized := u.String()
	path := filepath.Join(a.imageCacheDir(), imageCacheName(normalized))
	if a.serveCachedImage(w, path) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	data, err := a.fetchAndCacheImage(ctx, normalized, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", detectImageContentType(data))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
