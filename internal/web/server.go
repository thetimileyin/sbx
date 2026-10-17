// Package web serves the local page and its JSON API. It only answers requests
// addressed to this machine, so a web page elsewhere can't drive it (DNS rebinding).
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/thetimileyin/sbx/internal/engine"
)

//go:embed static
var static embed.FS

const maxBody = 4 << 20

func Handler(e *engine.Engine, port string) http.Handler {
	mux := http.NewServeMux()
	files, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServerFS(files))

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": e.Status(r.Context())})
	})

	// Probes are cheap but not free; cap concurrent checks.
	slots := make(chan struct{}, 2)
	mux.HandleFunc("POST /api/check", func(w http.ResponseWriter, r *http.Request) {
		var req engine.Request
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("expected JSON: {\"txs\": [...], \"mempool\": [...]}"))
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-r.Context().Done():
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		res, err := e.Check(ctx, req)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /api/examples", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, engine.Examples())
	})
	mux.HandleFunc("POST /api/examples/{id}", func(w http.ResponseWriter, r *http.Request) {
		req, err := e.BuildExample(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, req)
	})
	return guard(port, mux)
}

func guard(port string, next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		allowed[h+":"+port] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "sbx only answers requests to 127.0.0.1 or localhost", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "POST bodies must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// CheckAddr returns addr's port if addr is a loopback address.
func CheckAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("sbx serve only listens on a loopback address (127.0.0.1, ::1 or localhost)")
	}
	return port, nil
}

// Serve listens on addr, which must be a loopback address.
func Serve(ctx context.Context, e *engine.Engine, addr string) error {
	port, err := CheckAddr(addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(e, port),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
