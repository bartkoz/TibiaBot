package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io/fs"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

//go:embed web/*
var assets embed.FS

type server struct {
	dir          string
	gate         chan struct{}
	cached       *Atlas // Protected by gate; keep at most one loaded floor.
	debugDir     string
	lastDebug    time.Time
	localAtlases map[int]localAtlasEntry
	cacheClock   uint64
	// Route queries use their own lock and cache so they never contend with
	// the locate gate.
	costMu    sync.Mutex
	costCache *CostGrid
	costFloor int
	// Nil until -input selects an emitter; every input route then answers 503.
	driver *Driver
}

type matchRequest struct {
	Options
	Floor          int       `json:"floor"`
	Demo           bool      `json:"demo"`
	Near           *Position `json:"near,omitempty"`
	Radius         int       `json:"radius,omitempty"`
	NoPreview      bool      `json:"no_preview,omitempty"`
	AdjacentFloors bool      `json:"adjacent_floors,omitempty"`
	FloorRadius    int       `json:"floor_radius,omitempty"`
}

func main() {
	dir := flag.String("maps", "../data/minimap", "katalog z Minimap_Color_X_Y_Z.png")
	addr := flag.String("listen", "127.0.0.1:8095", "lokalny adres panelu")
	mode := flag.String("input", "off", "sterowanie: off, dry albo system")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("-listen musi wskazywać numeryczny adres loopback, np. 127.0.0.1:8095")
	}
	s := &server{dir: *dir, gate: make(chan struct{}, 1), debugDir: ".debug"}
	em, err := selectEmitter(*mode)
	if err != nil {
		log.Fatal(err)
	}
	if em != nil {
		s.driver = NewDriver(em)
		log.Printf("Sterowanie: %s — wykonawca startuje rozbrojony.", *mode)
	}
	log.Printf("Minimap Lab: http://%s — mapy: %s", *addr, *dir)
	h := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(h.ListenAndServe())
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/demo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, demoSnippet(demoAtlas()))
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

func (s *server) info(w http.ResponseWriter, r *http.Request) {
	floors := []int{}
	seen := map[int]bool{}
	entries, err := os.ReadDir(s.dir)
	for _, e := range entries {
		if m := chunkName.FindStringSubmatch(e.Name()); m != nil {
			z, _ := strconv.Atoi(m[3])
			if z >= 0 && z <= 15 && !seen[z] {
				seen[z] = true
				floors = append(floors, z)
			}
		}
	}
	sort.Ints(floors)
	message := ""
	if err != nil {
		message = "Brak katalogu map. Demo działa; własne mapy podaj przez -maps."
	}
	writeJSON(w, map[string]any{"floors": floors, "maps": s.dir, "message": message})
}

func (s *server) match(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "Nieprawidłowy formularz lub plik >8 MB", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	var req matchRequest
	if err := json.Unmarshal([]byte(r.FormValue("options")), &req); err != nil || req.Floor < 0 || req.Floor > 15 {
		http.Error(w, "Nieprawidłowe opcje lub piętro", 400)
		return
	}
	if req.Near != nil && (req.Near.Z < 0 || req.Near.Z > 15 || abs(req.Near.Z-req.Floor) > 1 || req.Near.X < 0 || req.Near.X > 65535 || req.Near.Y < 0 || req.Near.Y > 65535 || req.Zoom < 1 || req.Zoom > 8 || req.Radius < 1 || req.Radius > 64 || req.FloorRadius < 0 || req.FloorRadius > 32) {
		http.Error(w, "Nieprawidłowy obszar lokalny: wymagane Z lub Z±1, skala 1–8, promień ruchu 1–64 i promień przejścia 1–32 (0 = 8).", 400)
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Brak obrazu", 400)
		return
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width > 1024 || cfg.Height > 1024 || cfg.Width < 8 || cfg.Height < 8 {
		http.Error(w, "Wycinek PNG/JPEG musi mieć 8–1024 px na bok", 400)
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		http.Error(w, "Nie można odczytać obrazu", 400)
		return
	}
	im, _, err := image.Decode(f)
	if err != nil {
		http.Error(w, "Nie można odczytać obrazu", 400)
		return
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		http.Error(w, "Trwa inne wyszukiwanie. Spróbuj ponownie.", 429)
		return
	}
	// Throttle disk writes and logging during 5–10 Hz tracking.
	debugNow := s.debugDir != "" && !req.Demo && (req.Near == nil || time.Since(s.lastDebug) >= time.Second)
	if debugNow {
		s.lastDebug = time.Now()
		var capture bytes.Buffer
		png.Encode(&capture, im)
		s.saveDebug("last-input.png", capture.Bytes())
		s.saveDebug("last-options.json", []byte(r.FormValue("options")))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var atlas *Atlas
	matchStarted := time.Now()
	var result Result
	if req.Demo {
		atlas = demoAtlas()
	} else if req.Near != nil {
		result, atlas, err = s.locateLocal(ctx, im, req)
	} else {
		if s.cached == nil || s.cached.Floor != req.Floor {
			s.cached = nil
			s.cached, err = loadAtlas(s.dir, req.Floor)
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		atlas = s.cached
	}
	if req.Near != nil && req.Demo {
		result, err = locateNear(ctx, atlas, im, req.Options, *req.Near, req.Radius)
	} else if req.Near == nil {
		result, err = locateWithScale(ctx, atlas, im, req.Options)
	}
	result.MatchMS = float64(time.Since(matchStarted).Microseconds()) / 1000
	if err != nil {
		if debugNow {
			s.saveDebug("last-result.json", []byte(fmt.Sprintf("%q", err.Error())))
		}
		code := 400
		if ctx.Err() != nil {
			code = 408
		}
		http.Error(w, fmt.Sprintf("Wyszukiwanie przerwane: %v", err), code)
		return
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	if debugNow {
		data, _ := json.MarshalIndent(result, "", "  ")
		s.saveDebug("last-result.json", data)
		log.Printf("locate floor=%d zoom=%d crop=%dx%d found=%v best=%+v competitor=%+v elapsed=%dms", req.Floor, req.Zoom, im.Bounds().Dx(), im.Bounds().Dy(), result.Found, result.Best, result.Competitor, result.ElapsedMS)
	}
	preview := ""
	if result.Best != nil && atlas != nil && !req.NoPreview {
		p := image.Pt(result.Best.X, result.Best.Y).Sub(atlas.Origin)
		patch := image.NewNRGBA(image.Rect(0, 0, 129, 129))
		draw.Draw(patch, patch.Bounds(), atlas.Image, p.Sub(image.Pt(64, 64)), draw.Src)
		for d := -5; d <= 5; d++ {
			patch.Set(64+d, 64, color.NRGBA{255, 60, 90, 255})
			patch.Set(64, 64+d, color.NRGBA{255, 60, 90, 255})
		}
		var buf bytes.Buffer
		png.Encode(&buf, patch)
		preview = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}
	writeJSON(w, struct {
		Result
		Preview string `json:"preview"`
	}{result, preview})
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

func demoAtlas() *Atlas {
	im := image.NewNRGBA(image.Rect(0, 0, 384, 320))
	rng := rand.New(rand.NewSource(42))
	palette := []color.NRGBA{{30, 95, 40, 255}, {50, 125, 45, 255}, {80, 145, 55, 255}, {100, 100, 100, 255}, {160, 140, 70, 255}}
	for y := 0; y < 320; y++ {
		for x := 0; x < 384; x++ {
			c := palette[rng.Intn(len(palette))]
			if abs(x-90-y/3) < 7 {
				c = color.NRGBA{0, 100, 210, 255}
			}
			if y > 155 && y < 162 {
				c = color.NRGBA{190, 170, 105, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	return &Atlas{im, image.Pt(32000, 32000), 7}
}

func demoSnippet(a *Atlas) image.Image {
	// A 95×95-tile crop at zoom 2; player is at world (32200,32180,7).
	im := image.NewNRGBA(image.Rect(0, 0, 190, 190))
	for y := 0; y < 190; y++ {
		for x := 0; x < 190; x++ {
			im.Set(x, y, a.Image.At(153+x/2, 133+y/2))
		}
	}
	for d := -5; d <= 5; d++ {
		im.Set(94+d, 94, color.White)
		im.Set(94, 94+d, color.White)
	}
	return im
}
