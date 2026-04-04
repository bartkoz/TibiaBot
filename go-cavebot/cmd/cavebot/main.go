package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/anthropics/tibiabot/internal/core"
	"github.com/anthropics/tibiabot/internal/web"
)

func main() {
	configPath := flag.String("config", "configs/default.yaml", "Path to config YAML file")
	host := flag.String("host", "", "Web UI host (overrides config)")
	port := flag.Int("port", 0, "Web UI port (overrides config)")
	flag.Parse()

	cfg, err := core.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *host != "" {
		cfg.Web.Host = *host
	}
	if *port != 0 {
		cfg.Web.Port = *port
	}

	srv := web.NewServer(cfg)
	addr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
	log.Printf("Starting Tibia Cavebot on http://%s", addr)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatal(err)
	}
}
