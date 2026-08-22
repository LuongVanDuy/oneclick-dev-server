package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/LuongVanDuy/oneclick-dev-server/internal/platform"
	"github.com/LuongVanDuy/oneclick-dev-server/internal/project"
)

type Server struct {
	http *http.Server
	ln   net.Listener
}

func New(addr string, assets fs.FS) (*Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, platform.Detect())
	})
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, project.Discover())
	})
	mux.Handle("/", noCache(http.FileServer(http.FS(assets))))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}

	h := &http.Server{
		Addr:              addr,
		Handler:           requestLog(mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return &Server{http: h, ln: ln}, nil
}

func (s *Server) URL() string {
	return "http://" + s.ln.Addr().String()
}

func (s *Server) Serve() error {
	return s.http.Serve(s.ln)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
