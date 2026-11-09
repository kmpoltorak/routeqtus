// Package ui provides a local route-management interface backed by the Management API.
package ui

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kmpoltorak/routetus/internal/bootstrap"
	"github.com/kmpoltorak/routetus/internal/config"
	"github.com/kmpoltorak/routetus/internal/neterror"
)

//go:embed files/*
var assets embed.FS

var routeID = regexp.MustCompile(config.IdentifierPattern)

type Handler struct {
	api         *url.URL
	token, host string
	client      *http.Client
}

func New(apiURL, token, host string) (*Handler, error) {
	api, err := url.Parse(apiURL)
	if err != nil || api.Host == "" || api.User != nil || api.RawQuery != "" || api.ForceQuery || api.Fragment != "" || (api.Path != "" && api.Path != "/") || (api.Scheme != "https" && (api.Scheme != "http" || !bootstrap.Loopback(api.Host))) {
		return nil, fmt.Errorf("Management API must use HTTPS or numeric loopback HTTP, without credentials, a path, query, or fragment")
	}
	if !bootstrap.Loopback(host) {
		return nil, fmt.Errorf("GUI must listen on numeric loopback")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("management token environment reference must provide at least 32 bytes")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	return &Handler{api: api, token: token, host: host, client: &http.Client{
		Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (h *Handler) Close() { h.client.CloseIdleConnections() }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Host != h.host {
		problem(w, 403, "Unexpected GUI host")
		return
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || (len(origins) == 1 && origins[0] != "http://"+h.host) {
		problem(w, 403, "A same-origin request is required")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		problem(w, 403, "Cross-site API requests are not allowed")
		return
	}
	if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/routes/") {
		if r.Header.Get("Origin") != "http://"+h.host {
			problem(w, 403, "A same-origin request is required")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/routes/")
		if !routeID.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		h.proxy(w, r, "/v1/routes/"+id, nil)
		return
	}
	switch r.URL.Path {
	case "/", "/app.js", "/styles.css":
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			problem(w, 405, "Method not allowed")
			return
		}
		name := r.URL.Path[1:]
		contentType := "text/html; charset=utf-8"
		if name == "" {
			name = "index.html"
		} else if name == "app.js" {
			contentType = "application/javascript; charset=utf-8"
		} else {
			contentType = "text/css; charset=utf-8"
		}
		content, err := assets.ReadFile("files/" + name)
		if err != nil {
			problem(w, 500, "GUI asset unavailable")
			return
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
	case "/api/config", "/api/handler-types":
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			problem(w, 405, "Method not allowed")
			return
		}
		h.proxy(w, r, "/v1/"+strings.TrimPrefix(r.URL.Path, "/api/"), nil)
	case "/api/routes", "/api/upstreams", "/api/handlers":
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			problem(w, 405, "Method not allowed")
			return
		}
		if r.Header.Get("Origin") != "http://"+h.host {
			problem(w, 403, "A same-origin request is required")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			problem(w, 415, "A JSON request is required")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			problem(w, 413, "Configuration document exceeds the request limit or could not be read")
			return
		}
		h.proxy(w, r, "/v1/"+strings.TrimPrefix(r.URL.Path, "/api/"), bytes.NewReader(body))
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request, path string, requestBody io.Reader) {
	address := *h.api
	address.Path, address.RawPath = path, ""
	request, err := http.NewRequestWithContext(r.Context(), r.Method, address.String(), requestBody)
	if err != nil {
		problem(w, 502, "Management API request could not be prepared")
		return
	}
	request.Header.Set("Authorization", "Bearer "+h.token)
	request.Header.Set("Accept", "application/json")
	if r.Method == "POST" {
		request.Header.Set("Content-Type", "application/json")
	}
	if r.Method == "POST" || r.Method == "DELETE" {
		request.Header.Set("If-Match", r.Header.Get("If-Match"))
	}
	response, err := h.client.Do(request)
	if err != nil {
		problem(w, 502, "Management API unavailable: "+neterror.Reason(err))
		return
	}
	defer response.Body.Close()
	const maxResponseBytes = 64 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		problem(w, 502, "Management API response could not be read within limits")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if etag := response.Header.Get("ETag"); etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(body)
}

func problem(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
