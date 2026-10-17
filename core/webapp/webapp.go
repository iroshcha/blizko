// Package webapp embeds a self-contained PWA. No dependency is loaded from a CDN.
package webapp

import (
	"embed"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
)

//go:embed assets/*
var assets embed.FS

func ValidateOrigin(raw string) error {
	if strings.Contains(raw, ",") {
		for _, origin := range strings.Split(raw, ",") {
			if origin == "" {
				return errors.New("empty web origin")
			}
			if err := ValidateOrigin(origin); err != nil {
				return err
			}
		}
		return nil
	}
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.String() != raw {
		return errors.New("web-origin must be an exact origin without a path")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("web-origin requires HTTPS (HTTP allowed only on literal loopback for tests)")
}

func Handler(origin string) http.Handler {
	files, _ := fs.Sub(assets, "assets")
	socket := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(origin, ",", " "), "https://", "wss://"), "http://", "ws://")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; connect-src 'self' "+socket+"; worker-src 'self'; manifest-src 'self'; media-src 'self' blob:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/web/")
		if name == "" {
			name = "index.html"
		}
		if name != path.Base(name) || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(files, name); err != nil {
			http.NotFound(w, r)
			return
		}
		if name == "sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/web/")
		}
		// Do not depend on MIME mappings in the host's Windows registry.
		types := map[string]string{".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".html": "text/html; charset=utf-8", ".webmanifest": "application/manifest+json", ".svg": "image/svg+xml", ".png": "image/png", ".txt": "text/plain; charset=utf-8"}
		if kind := types[path.Ext(name)]; kind != "" {
			w.Header().Set("Content-Type", kind)
		}
		request := r.Clone(r.Context())
		copyURL := *r.URL
		request.URL = &copyURL
		request.URL.Path = "/" + name
		if name == "index.html" {
			request.URL.Path = "/"
		}
		http.FileServer(http.FS(files)).ServeHTTP(w, request)
	})
}
