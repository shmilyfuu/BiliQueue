package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FontFile struct {
	File  string `json:"file"`
	Label string `json:"label"`
}

var supportedFontExtensions = map[string]bool{
	".ttf":   true,
	".otf":   true,
	".woff":  true,
	".woff2": true,
}

func normalizeFontFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return ""
	}
	if !supportedFontExtensions[strings.ToLower(filepath.Ext(name))] {
		return ""
	}
	return name
}

func (a *App) listFonts() []FontFile {
	entries, err := os.ReadDir(a.fontsDir)
	if err != nil {
		return []FontFile{}
	}
	fonts := make([]FontFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := normalizeFontFileName(entry.Name())
		if name == "" {
			continue
		}
		label := strings.TrimSuffix(name, filepath.Ext(name))
		fonts = append(fonts, FontFile{File: name, Label: label})
	}
	sort.Slice(fonts, func(i, j int) bool {
		return strings.ToLower(fonts[i].File) < strings.ToLower(fonts[j].File)
	})
	return fonts
}

func (a *App) handleFonts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"fonts": a.listFonts()})
}

func (a *App) handleFontFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/fonts/")
	name, err := url.PathUnescape(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name = normalizeFontFileName(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.fontsDir, name)
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, name, info.ModTime(), file)
}
