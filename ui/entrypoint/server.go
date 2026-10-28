package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// config is the route table the chart renders into the ui-config ConfigMap.
type config struct {
	Listen            string  `json:"listen"`
	ClientMaxBodySize string  `json:"clientMaxBodySize"`
	ProxyReadTimeout  string  `json:"proxyReadTimeout"`
	ProxySendTimeout  string  `json:"proxySendTimeout"`
	Proxies           []proxy `json:"proxies"`
}

// proxy forwards a ServeMux pattern to a backend. A header set to "" is removed, and
// values expand $host, $remote_addr, $proxy_add_x_forwarded_for, $scheme and $server_name.
type proxy struct {
	Path    string            `json:"path"`
	Backend string            `json:"backend"`
	Headers map[string]string `json:"headers"`
}

func loadConfig(path string) (config, error) {
	cfg := config{Listen: ":8080", ClientMaxBodySize: "1m", ProxyReadTimeout: "60s", ProxySendTimeout: "60s"}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(data, &cfg)
}

func newHandler(cfg config, root, generated, basePath string) (http.Handler, error) {
	maxBody, err := parseSize(cfg.ClientMaxBodySize)
	if err != nil {
		return nil, err
	}
	read, err := parseDuration(cfg.ProxyReadTimeout)
	if err != nil {
		return nil, err
	}
	send, err := parseDuration(cfg.ProxySendTimeout)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "OK") })
	mux.HandleFunc("/env-config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript")
		serveFile(w, r, generated+"/env-config.js")
	})
	// The bundle entrypoint is never cached, or a client keeps loading the previous release's assets.
	index := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		serveFile(w, r, generated+"/index.html")
	}
	mux.HandleFunc("/index.html", index)
	// The image ships no mock service worker; the SPA fallback would answer it with a misleading 200.
	mux.Handle("/mockServiceWorker.js", http.NotFoundHandler())
	// Vite fingerprints these filenames, so a given URL's bytes never change.
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		serveFile(w, r, root+r.URL.Path)
	})
	// SPA fallback: a deep link such as /agents is a client-side route, not a file.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if fi, err := os.Stat(root + r.URL.Path); err == nil && fi.Mode().IsRegular() {
			serveFile(w, r, root+r.URL.Path)
			return
		}
		index(w, r)
	})

	for _, p := range cfg.Proxies {
		backend, err := url.Parse(p.Backend)
		if err != nil {
			return nil, fmt.Errorf("proxy %s: %w", p.Path, err)
		}
		mux.Handle(p.Path, limitBody(idleTimeout(newProxy(backend, p.Headers), read, send), maxBody))
	}
	return stripBasePath(mux, basePath), nil
}

func newProxy(backend *url.URL, headers map[string]string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(backend)
			host, _, err := net.SplitHostPort(pr.In.Host)
			if err != nil {
				host = pr.In.Host
			}
			remote, _, _ := net.SplitHostPort(pr.In.RemoteAddr)
			xff := remote
			if prior := pr.In.Header.Get("X-Forwarded-For"); prior != "" {
				xff = prior + ", " + remote
			}
			vars := map[string]string{"host": host, "remote_addr": remote, "proxy_add_x_forwarded_for": xff, "scheme": "http", "server_name": "localhost"}
			for name, value := range headers {
				value = os.Expand(value, func(k string) string { return vars[k] })
				switch {
				case strings.EqualFold(name, "Host"):
					pr.Out.Host = value
				case value == "":
					pr.Out.Header.Del(name)
				default:
					pr.Out.Header.Set(name, value)
				}
			}
		},
		// Chat is server-sent events; buffering would hold tokens back.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			fmt.Fprintf(os.Stderr, "entrypoint: proxy %s: %v\n", r.URL.Path, err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
}

// idleTimeout cancels a request once the backend neither reads nor writes for the
// send or read timeout, like nginx's proxy_send_timeout and proxy_read_timeout.
func idleTimeout(next http.Handler, read, send time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An upgraded connection outlives the request, so cancelling it would cut the socket.
		if r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		timer := time.AfterFunc(read, cancel)
		defer timer.Stop()
		r = r.WithContext(ctx)
		r.Body = &idleBody{ReadCloser: r.Body, timer: timer, send: send, read: read}
		next.ServeHTTP(&idleWriter{ResponseWriter: w, timer: timer, read: read}, r)
	})
}

type idleBody struct {
	io.ReadCloser
	timer      *time.Timer
	send, read time.Duration
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.timer.Reset(b.read)
	} else {
		b.timer.Reset(b.send)
	}
	return n, err
}

type idleWriter struct {
	http.ResponseWriter
	timer *time.Timer
	read  time.Duration
}

func (w *idleWriter) Write(p []byte) (int, error) {
	w.timer.Reset(w.read)
	return w.ResponseWriter.Write(p)
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// limitBody answers 413 for a body over max, like nginx's client_max_body_size; 0 disables it.
func limitBody(next http.Handler, max int64) http.Handler {
	if max == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > max {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		next.ServeHTTP(w, r)
	})
}

// stripBasePath drops ui.basePath when the proxy in front forwards it unstripped.
func stripBasePath(next http.Handler, basePath string) http.Handler {
	if basePath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rest, ok := strings.CutPrefix(r.URL.Path, basePath); ok && (rest == "" || rest[0] == '/') {
			r.URL.Path, r.URL.RawPath = "/"+strings.TrimPrefix(rest, "/"), ""
		}
		next.ServeHTTP(w, r)
	})
}

// serveFile serves a regular file, unlike http.ServeFile, which redirects /index.html.
func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := os.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// parseSize reads an nginx size such as 20m or 512k.
func parseSize(s string) (int64, error) {
	mult := int64(1)
	switch strings.ToLower(s[len(s)-1:]) {
	case "k":
		mult = 1 << 10
	case "m":
		mult = 1 << 20
	case "g":
		mult = 1 << 30
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n * mult, err
}

// parseDuration reads an nginx time such as 1800s or 30m; a bare number is seconds.
func parseDuration(s string) (time.Duration, error) {
	if _, err := strconv.Atoi(s); err == nil {
		s += "s"
	}
	return time.ParseDuration(s)
}
