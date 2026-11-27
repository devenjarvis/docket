package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// LocalOnly guards a handler that listens on a local port, where any web page
// the user visits can send it requests. It refuses a Host that is not this
// computer (DNS rebinding), a request another site started, and a change
// that is not JSON, which a page cannot send without the browser asking
// first.
func LocalOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			http.Error(w, "Docket answers only on this computer's own address.", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				http.Error(w, "Docket does not answer other sites.", http.StatusForbidden)
				return
			}
		}
		if s := r.Header.Get("Sec-Fetch-Site"); s == "cross-site" || s == "same-site" {
			http.Error(w, "Docket does not answer other sites.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "Docket takes changes only as JSON.", http.StatusUnsupportedMediaType)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
