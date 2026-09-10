package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoundary(t *testing.T) {
	s := &Server{Origin: "http://127.0.0.1:8318", Token: "private", ctx: context.Background()}
	h := s.Router()
	for _, v := range []struct {
		method, path, host, origin, token string
		code                              int
	}{
		{"GET", "/api/session", "evil.example:8318", "", "", 403},
		{"GET", "/api/services", "127.0.0.1:8318", "", "", 403},
		{"POST", "/api/management-key", "127.0.0.1:8318", "http://evil.example", "private", 403},
		{"GET", "/api/session", "127.0.0.1:8318", "", "", 200},
		{"GET", "/", "127.0.0.1:8318", "", "", 200},
	} {
		r := httptest.NewRequest(v.method, "http://"+v.host+v.path, strings.NewReader("{}"))
		r.Host = v.host
		r.Header.Set("Origin", v.origin)
		r.Header.Set("X-Runtime-Token", v.token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != v.code {
			t.Fatalf("%s: %d", v.path, w.Code)
		}
	}
}
