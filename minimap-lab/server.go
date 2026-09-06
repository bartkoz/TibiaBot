package main

import (
	"embed"
	"encoding/json"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minimap-lab/internal/input"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
)

//go:embed web/*
var assets embed.FS

type server struct {
	dir          string
	gate         chan struct{}
	cached       *mapdata.Atlas // Protected by gate; keep at most one loaded floor.
	debugDir     string
	lastDebug    time.Time
	localAtlases map[int]localAtlasEntry
	cacheClock   uint64
	// Route queries use their own lock and cache so they never contend with
	// the locate gate.
	costMu    sync.Mutex
	costCache *mapdata.CostGrid
	costFloor int
	// The live preview asks for a small window around the character while the
	// planner asks for a rectangle spanning a whole route. One shared cache
	// would have them evict each other on every single reading.
	previewMu    sync.Mutex
	previewCache *mapdata.CostGrid
	previewFloor int
	// Nil until -input selects an emitter; every input route then answers 503.
	driver *input.Driver
	// Learned blockages: tiles the map data calls walkable but the character
	// cannot actually enter. Nil in tests that predate the store; every method
	// on it tolerates a nil receiver.
	blocks *nav.BlockStore
	// Collapses identical intent log lines so a refusal repeating at the
	// tracking rate does not drown the log.
	repeatMu   sync.Mutex
	lastLogged string
	repeats    int
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/demo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, mapdata.DemoSnippet(mapdata.DemoAtlas()))
	})
	mux.HandleFunc("POST /api/locate", s.match)
	mux.HandleFunc("POST /api/path", s.path)
	mux.HandleFunc("POST /api/arm", s.arm)
	mux.HandleFunc("POST /api/disarm", s.disarm)
	mux.HandleFunc("POST /api/input", s.input)
	mux.HandleFunc("POST /api/input/calibrate", s.calibrate)
	mux.HandleFunc("POST /api/input/config", s.inputConfig)
	mux.HandleFunc("POST /api/input/done", s.actionDone)
	mux.HandleFunc("GET /api/input/status", s.inputStatus)
	mux.HandleFunc("POST /api/blocks/observe", s.observeBlock)
	mux.HandleFunc("GET /api/blocks", s.listBlocks)
	mux.HandleFunc("DELETE /api/blocks", s.deleteBlock)
	mux.HandleFunc("GET /api/grid", s.grid)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No cross-origin uploads or remote DNS names on the local service.
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "local host required", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "same origin required", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (s *server) saveDebug(name string, data []byte) {
	if s.debugDir == "" {
		return
	}
	if err := os.MkdirAll(s.debugDir, 0700); err != nil {
		log.Printf("debug: %v", err)
		return
	}
	path := filepath.Join(s.debugDir, name)
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		log.Printf("debug: %v", err)
		return
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		log.Printf("debug: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// writeJSONError answers with a JSON body carrying reason, at the given
// status. Unlike http.Error's plain text, this survives the panel's
// postSafe(), which always calls response.json() - a plain-text body would
// fail to parse there and silently discard the actual reason behind a
// generic parse-error message instead.
func writeJSONError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"reason": reason})
}
