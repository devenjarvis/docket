package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalOnly(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := LocalOnly(ok)
	cases := []struct {
		name   string
		method string
		host   string
		header map[string]string
		want   int
	}{
		{"the page itself", "POST", "127.0.0.1:4127", map[string]string{"Content-Type": "application/json", "Origin": "http://127.0.0.1:4127", "Sec-Fetch-Site": "same-origin"}, 200},
		{"localhost", "GET", "localhost:4127", nil, 200},
		{"a rebound name", "GET", "evil.example:4127", nil, 403},
		{"another site's form", "POST", "127.0.0.1:4127", map[string]string{"Content-Type": "text/plain"}, 415},
		{"another site's fetch", "POST", "127.0.0.1:4127", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, 403},
		{"cross-site without an origin", "POST", "127.0.0.1:4127", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}, 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/api/act", strings.NewReader("{}"))
			r.Host = c.host
			for k, v := range c.header {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Errorf("status = %d, want %d", w.Code, c.want)
			}
		})
	}
}
