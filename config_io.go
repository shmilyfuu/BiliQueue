package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxImportedConfigBytes = 2 << 20

func (a *App) configBackupDir() string {
	return filepath.Join(a.dataDir, "backups")
}

func (a *App) backupCurrentConfig(prefix string) (string, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()
	if err := os.MkdirAll(a.configBackupDir(), 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.json", prefix, time.Now().Format("20060102-150405.000"))
	path := filepath.Join(a.configBackupDir(), name)
	if err := writeJSONAtomic(path, cfg); err != nil {
		return "", err
	}
	return path, nil
}

func decodeImportedConfig(r io.Reader) (Config, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxImportedConfigBytes+1))
	if err != nil {
		return Config{}, err
	}
	if len(data) == 0 || len(data) > maxImportedConfigBytes {
		return Config{}, errors.New("配置文件为空或超过 2MB")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return Config{}, errors.New("配置文件不是有效的 JSON")
	}
	known := false
	for _, key := range []string{"schemaVersion", "roomId", "joinCommand", "cancelCommand", "maxQueue", "giftPriority", "overlay"} {
		if _, ok := probe[key]; ok {
			known = true
			break
		}
	}
	if !known {
		return Config{}, errors.New("文件中没有识别到 BiliQueue 配置字段")
	}
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, errors.New("配置字段格式不正确")
	}
	applyConfigDefaults(&cfg)
	cfg.SchemaVersion = defaultConfig().SchemaVersion
	return cfg, nil
}

func (a *App) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()
	cfg.SchemaVersion = defaultConfig().SchemaVersion
	name := fmt.Sprintf("BiliQueue-config-%s.json", time.Now().Format("2006-01-02-150405"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(cfg)
}

func (a *App) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	cfg, err := decodeImportedConfig(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	backupPath, err := a.backupCurrentConfig("config-before-import")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "备份当前配置失败：" + err.Error()})
		return
	}
	a.mu.Lock()
	a.config = cfg
	a.normalizePriorityZoneLocked()
	a.mu.Unlock()
	a.saveConfig()
	a.saveQueue()
	a.applyHotkeys(cfg.Hotkeys)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"backupFile": filepath.Base(backupPath),
		"state":      a.state(),
	})
}
