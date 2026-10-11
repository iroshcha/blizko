package webapp

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactOriginAndPrivateFiles(t *testing.T) {
	for _, origin := range []string{"https://chat.example", "http://127.0.0.1:3340", ""} {
		if err := ValidateOrigin(origin); err != nil {
			t.Fatal(origin, err)
		}
	}
	for _, origin := range []string{"https://chat.example/", "https://user@chat.example", "http://chat.example", "https://chat.example?", "null", "https://chat.example#x"} {
		if ValidateOrigin(origin) == nil {
			t.Fatal("allowed invalid origin", origin)
		}
	}
	h := Handler("https://chat.example")
	for _, p := range []string{"/web/", "/web/app.js", "/web/sw.js", "/web/manifest.webmanifest", "/web/icon-192.png"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(p, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing security headers")
		}
		if p == "/web/manifest.webmanifest" && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/manifest+json") {
			t.Fatal("manifest must have its PWA MIME type")
		}
	}
	for _, p := range []string{"/web/../access-code.txt", "/web/.env", "/web/unknown", "/web/assets/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatal("exposed path", p, w.Code)
		}
	}
}
