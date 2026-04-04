package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anthropics/tibiabot/internal/core"
	"github.com/anthropics/tibiabot/internal/navigation"
	"github.com/gorilla/websocket"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	Config  *core.BotConfig
	Atlas   *navigation.Atlas
	Bot     *core.CaveBot
	Manager *ConnectionManager
	mux     *http.ServeMux
}

func NewServer(config *core.BotConfig) *Server {
	s := &Server{
		Config:  config,
		Manager: NewConnectionManager(),
		mux:     http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) setupRoutes() {
	staticSub, _ := fs.Sub(staticFS, "static")
	staticHandler := http.FileServer(http.FS(staticSub))

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			data, err := staticFS.ReadFile("static/index.html")
			if err != nil {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, "<html><body><h1>Cavebot</h1><p>Static files not found.</p></body></html>")
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	})

	s.mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))

	s.mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Config.ToMap())
	})

	s.mux.HandleFunc("/api/bot/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		if s.Atlas == nil {
			atlas := navigation.NewAtlas(s.Config.Minimap.DataPath)
			if err := atlas.Load(); err != nil {
				log.Printf("Atlas load warning: %v", err)
			}
			s.Atlas = atlas
		}

		bot := core.NewCaveBot(s.Config, s.Atlas, func(status map[string]interface{}) {
			s.Manager.Broadcast(status)
		})
		bot.Start()
		s.Bot = bot
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})

	s.mux.HandleFunc("/api/bot/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		if s.Bot != nil {
			s.Bot.Stop()
			s.Bot = nil
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})

	s.mux.HandleFunc("/api/bot/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if s.Bot != nil {
			json.NewEncoder(w).Encode(s.Bot.Status())
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{"state": "IDLE", "running": false})
		}
	})

	s.mux.HandleFunc("/api/atlas/bounds/", func(w http.ResponseWriter, r *http.Request) {
		zStr := strings.TrimPrefix(r.URL.Path, "/api/atlas/bounds/")
		z, err := strconv.Atoi(zStr)
		if err != nil {
			http.Error(w, "invalid z", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if s.Atlas == nil {
			json.NewEncoder(w).Encode(navigation.Bounds{})
			return
		}
		json.NewEncoder(w).Encode(s.Atlas.WorldBounds(z))
	})

	s.mux.HandleFunc("/api/atlas/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		suffix := strings.TrimPrefix(path, "/api/atlas/")
		if strings.HasPrefix(suffix, "bounds/") {
			return // handled by bounds handler
		}
		z, err := strconv.Atoi(suffix)
		if err != nil {
			http.Error(w, "invalid z", 400)
			return
		}
		if s.Atlas == nil {
			w.Header().Set("Content-Type", "image/png")
			return
		}
		bounds := s.Atlas.WorldBounds(z)
		if bounds.MaxX == 0 {
			w.Header().Set("Content-Type", "image/png")
			return
		}
		imgW := bounds.MaxX - bounds.MinX
		imgH := bounds.MaxY - bounds.MinY
		canvas := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

		for key, tile := range s.Atlas.ColorChunks {
			if key.Z != z {
				continue
			}
			ox := key.X - bounds.MinX
			oy := key.Y - bounds.MinY
			tileBounds := tile.Bounds()
			for row := 0; row < tileBounds.Dy(); row++ {
				for col := 0; col < tileBounds.Dx(); col++ {
					c := tile.RGBAAt(col, row)
					canvas.SetRGBA(ox+col, oy+row, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
				}
			}
		}

		var buf bytes.Buffer
		png.Encode(&buf, canvas)
		w.Header().Set("Content-Type", "image/png")
		w.Write(buf.Bytes())
	})

	s.mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.Manager.Add(conn)
		defer func() {
			s.Manager.Remove(conn)
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
