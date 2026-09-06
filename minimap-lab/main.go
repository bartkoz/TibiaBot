package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"minimap-lab/internal/input"
	"minimap-lab/internal/nav"
)

func main() {
	dir := flag.String("maps", "../data/minimap", "katalog z Minimap_Color_X_Y_Z.png")
	addr := flag.String("listen", "127.0.0.1:8095", "lokalny adres panelu")
	mode := flag.String("input", "off", "sterowanie: off, dry albo system")
	blocksPath := flag.String("blocks", "blocks.json",
		"plik nauczonych blokad; katalog map to pobrana paczka i nasze wpisy nie powinny się z nią mieszać")
	staleMS := flag.Int("stale-ms", input.DefaultMaxObservationAgeMS,
		fmt.Sprintf("maksymalny wiek obserwacji pozycji w ms (%d–%d); wolny \"Cały odczyt\" w panelu przekraczający tę wartość odrzuca każdy krok", input.MinStaleMS, input.MaxStaleMS))
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("-listen musi wskazywać numeryczny adres loopback, np. 127.0.0.1:8095")
	}
	if err := input.ValidateStaleMS(*staleMS); err != nil {
		log.Fatal(err)
	}
	s := &server{dir: *dir, gate: make(chan struct{}, 1), debugDir: ".debug"}
	s.blocks = nav.NewBlockStore(time.Now)
	s.blocks.SetPath(*blocksPath)
	// Fatal, not a warning: starting with an empty overlay would silently throw
	// away knowledge the user believes is saved, and the next Save would then
	// overwrite the file with nothing.
	if err := s.blocks.Load(); err != nil {
		log.Fatal(err)
	}
	em, err := input.SelectEmitter(*mode)
	if err != nil {
		log.Fatal(err)
	}
	if em != nil {
		s.driver = input.NewDriver(em, *staleMS)
		log.Printf("Sterowanie: %s — wykonawca startuje rozbrojony. Próg świeżości: %d ms.", *mode, *staleMS)
	}
	log.Printf("Minimap Lab: http://%s — mapy: %s", *addr, *dir)
	h := &http.Server{Addr: *addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(h.ListenAndServe())
}
