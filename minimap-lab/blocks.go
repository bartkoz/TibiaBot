package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Policy constants for learned blockages. They live together because they are
// one policy, not six unrelated numbers.
const (
	// tempTTL is how long a freshly learned block steers routes away. It is
	// deliberately short: the cause may well have been a player who has since
	// walked off.
	tempTTL = 60 * time.Second
	// edgeTTL is shorter still. An edge block only stops the bot retrying the
	// same corner in a loop; it says nothing about the terrain.
	edgeTTL = 20 * time.Second
	// forgetAfter is how long the record itself survives. It outlives the
	// block on purpose: without a memory of the first episode, a second one
	// could never be recognised as second and nothing would ever be promoted.
	forgetAfter = 24 * time.Hour
	// tempPenalty is added to the tile cost while a temporary block holds. A
	// penalty rather than a wall, so a player standing in a one-tile doorway
	// makes the bot wait and retry instead of declaring the route impossible.
	tempPenalty = 500
	// minStillFrames and maxFrameAgeMS are the evidence a bump must carry. One
	// stale frame is not proof that the character stayed put.
	minStillFrames = 3
	maxFrameAgeMS  = 300
)

type Kind uint8

const (
	KindNone Kind = iota
	KindTemp
	KindPerm
)

// Observation is what the panel saw after a single step attempt. Outcome is
// "no_motion" (the character stayed on From) or "entered" (it reached To after
// all, which revokes whatever was learned about that tile).
type Observation struct {
	From           Position `json:"from"`
	To             Position `json:"to"`
	Outcome        string   `json:"outcome"`
	StillFrames    int      `json:"still_frames"`
	LastFrameAgeMS int      `json:"last_frame_age_ms"`
}

// Decision is the server's answer. Result is ignored, temp, promoted or
// cleared; Reason always explains it, so a refused observation never looks
// like a dropped request in the panel.
type Decision struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

type Blockage struct {
	Kind     Kind
	Episodes int
	First    time.Time
	Expires  time.Time // zero for KindPerm: a permanent block does not expire
	Forget   time.Time
}

type tileKey [3]int // x, y, z
type edgeKey [5]int // fromX, fromY, toX, toY, z

type BlockStore struct {
	mu    sync.RWMutex
	now   func() time.Time
	tiles map[tileKey]*Blockage
	edges map[edgeKey]*Blockage
	rev   uint64
	// path and dirty back the on-disk file of permanent blocks. dirty is set
	// only by changes worth persisting, so a burst of temporary blocks never
	// rewrites the file.
	path  string
	dirty bool
}

// Overlay is an immutable view of the blockages inside one area of one floor,
// taken before a search starts. A* assumes the cost of a closed vertex never
// changes, so the graph must not shift under it mid-search.
type Overlay struct {
	tiles map[[2]int]Kind
	edges map[[4]int]bool
}

func (o *Overlay) Tile(x, y int) Kind {
	if o == nil {
		return KindNone
	}
	return o.tiles[[2]int{x, y}]
}

func (o *Overlay) Edge(fx, fy, tx, ty int) bool {
	if o == nil {
		return false
	}
	return o.edges[[4]int{fx, fy, tx, ty}]
}

func NewBlockStore(now func() time.Time) *BlockStore {
	return &BlockStore{now: now, tiles: map[tileKey]*Blockage{}, edges: map[edgeKey]*Blockage{}}
}

func (s *BlockStore) Revision() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rev
}

func (s *BlockStore) Observe(obs Observation) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)

	switch obs.Outcome {
	case "entered":
		if s.clearLocked(tileKey{obs.To.X, obs.To.Y, obs.To.Z}) {
			s.rev++
			return Decision{Result: "cleared", Reason: "Postać weszła na kratkę; blokada usunięta."}
		}
		return Decision{Result: "ignored", Reason: "Kratka nie była zablokowana."}
	case "no_motion":
	default:
		return Decision{Result: "ignored", Reason: fmt.Sprintf("Nieznany wynik próby: %s", obs.Outcome)}
	}

	if obs.From.Z != obs.To.Z {
		return Decision{Result: "ignored", Reason: "Krok między piętrami nie jest dowodem przeszkody."}
	}
	dx, dy := abs(obs.To.X-obs.From.X), abs(obs.To.Y-obs.From.Y)
	if dx > 1 || dy > 1 || (dx == 0 && dy == 0) {
		return Decision{Result: "ignored", Reason: "Kratki nie sąsiadują ze sobą."}
	}
	if obs.StillFrames < minStillFrames {
		return Decision{Result: "ignored",
			Reason: fmt.Sprintf("Za mało klatek bez ruchu: %d, wymagane %d.", obs.StillFrames, minStillFrames)}
	}
	if obs.LastFrameAgeMS > maxFrameAgeMS {
		return Decision{Result: "ignored",
			Reason: fmt.Sprintf("Ostatnia klatka starsza niż %d ms.", maxFrameAgeMS)}
	}

	// A diagonal step also fails when both orthogonal tiles at the corner are
	// blocked, and then the target itself is often perfectly walkable. Learning
	// tiles from diagonals is how a bot slowly carves working rooms out of its
	// own map, so a diagonal only ever blocks the edge it failed on.
	if dx == 1 && dy == 1 {
		k := edgeKey{obs.From.X, obs.From.Y, obs.To.X, obs.To.Y, obs.From.Z}
		s.edges[k] = &Blockage{Kind: KindTemp, Episodes: 1, First: now,
			Expires: now.Add(edgeTTL), Forget: now.Add(forgetAfter)}
		s.rev++
		return Decision{Result: "temp", Reason: "Skos nieprzejezdny; zablokowano samo przejście."}
	}

	k := tileKey{obs.To.X, obs.To.Y, obs.To.Z}
	b := s.tiles[k]
	if b == nil {
		s.tiles[k] = &Blockage{Kind: KindTemp, Episodes: 1, First: now,
			Expires: now.Add(tempTTL), Forget: now.Add(forgetAfter)}
		s.rev++
		return Decision{Result: "temp", Reason: "Pierwszy epizod; blokada tymczasowa."}
	}
	if b.Kind == KindPerm {
		b.Forget = now.Add(forgetAfter)
		return Decision{Result: "promoted", Reason: "Kratka była już blokadą trwałą."}
	}
	// Still inside the previous block's lifetime: this is the same episode
	// seen again (a retry, a recomputed route, a resent report), not a second
	// independent one.
	if now.Before(b.Expires) {
		b.Forget = now.Add(forgetAfter)
		return Decision{Result: "temp", Reason: "Ten sam epizod; blokada tymczasowa przedłużona."}
	}
	b.Episodes++
	b.Forget = now.Add(forgetAfter)
	if b.Episodes >= 2 {
		b.Kind, b.Expires = KindPerm, time.Time{}
		s.dirty = true
		s.rev++
		return Decision{Result: "promoted", Reason: "Drugi niezależny epizod; blokada trwała."}
	}
	b.Kind = KindTemp
	b.Expires = now.Add(tempTTL)
	s.rev++
	return Decision{Result: "temp", Reason: "Kolejny epizod; blokada tymczasowa."}
}

// Clear removes whatever is known about a tile. Reported presence beats any
// learned hypothesis, so this also drops the episode count - otherwise the
// next single bump would promote straight to permanent.
func (s *BlockStore) Clear(p Position) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clearLocked(tileKey{p.X, p.Y, p.Z}) {
		s.rev++
		return true
	}
	return false
}

func (s *BlockStore) clearLocked(k tileKey) bool {
	b, ok := s.tiles[k]
	if !ok {
		return false
	}
	if b.Kind == KindPerm {
		s.dirty = true
	}
	delete(s.tiles, k)
	return true
}

func (s *BlockStore) Snapshot(area image.Rectangle, z int) *Overlay {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	o := &Overlay{tiles: map[[2]int]Kind{}, edges: map[[4]int]bool{}}
	for k, b := range s.tiles {
		// KindNone is an expired block whose record is still remembered for
		// episode counting; it must not steer any route.
		if b.Kind == KindNone || k[2] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		o.tiles[[2]int{k[0], k[1]}] = b.Kind
	}
	for k := range s.edges {
		if k[4] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		o.edges[[4]int{k[0], k[1], k[2], k[3]}] = true
	}
	return o
}

// sweepLocked drops expired blocks and forgotten records. Expiry and
// forgetting are separate: a block stops steering routes long before its
// record is gone, and the record is what makes a second episode countable.
func (s *BlockStore) sweepLocked(now time.Time) {
	for k, b := range s.tiles {
		// A permanent block is never forgotten by time. Forget only bounds how
		// long an expired temporary record keeps its episode count around, so
		// a bump years later starts from scratch rather than promoting at once.
		if b.Kind != KindPerm && now.After(b.Forget) {
			delete(s.tiles, k)
			s.rev++
			continue
		}
		if b.Kind == KindTemp && !now.Before(b.Expires) {
			b.Kind = KindNone
			s.rev++
		}
	}
	for k, b := range s.edges {
		if now.After(b.Forget) || !now.Before(b.Expires) {
			delete(s.edges, k)
			s.rev++
		}
	}
}

// blocksFileVersion guards the on-disk format. A file from a newer version is
// refused rather than read partially - Save would then overwrite data written
// by a build that understood more than this one does.
const blocksFileVersion = 1

type blocksFile struct {
	Version int          `json:"version"`
	Tiles   []storedTile `json:"tiles"`
}

type storedTile struct {
	X         int       `json:"x"`
	Y         int       `json:"y"`
	Z         int       `json:"z"`
	Episodes  int       `json:"episodes"`
	FirstSeen time.Time `json:"first_seen"`
}

func (s *BlockStore) SetPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
}

func (s *BlockStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // a first run simply has nothing to load
	}
	if err != nil {
		return err
	}
	var f blocksFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("nie udało się odczytać %s: %w", s.path, err)
	}
	if f.Version != blocksFileVersion {
		return fmt.Errorf("plik blokad %s ma wersję %d, obsługiwana jest %d", s.path, f.Version, blocksFileVersion)
	}
	now := s.now()
	for _, t := range f.Tiles {
		s.tiles[tileKey{t.X, t.Y, t.Z}] = &Blockage{Kind: KindPerm, Episodes: t.Episodes,
			First: t.FirstSeen, Forget: now.Add(forgetAfter)}
	}
	s.rev++
	return nil
}

// Save rewrites the file through a temporary in the same directory followed by
// a rename, so a crash mid-write cannot leave a half-written file behind. Only
// permanent blocks are written: a temporary one is a guess with a minute to
// live and has no business outliving the process.
func (s *BlockStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *BlockStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	f := blocksFile{Version: blocksFileVersion, Tiles: []storedTile{}}
	for k, b := range s.tiles {
		if b.Kind != KindPerm {
			continue
		}
		f.Tiles = append(f.Tiles, storedTile{X: k[0], Y: k[1], Z: k[2], Episodes: b.Episodes, FirstSeen: b.First})
	}
	// Stable order keeps the file diffable and its rewrites boring.
	sort.Slice(f.Tiles, func(i, j int) bool {
		a, b := f.Tiles[i], f.Tiles[j]
		if a.Z != b.Z {
			return a.Z < b.Z
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.X < b.X
	})
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".blocks-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeded
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Flush writes the file if a permanent block changed since the last write. The
// caller is the HTTP handler, so a burst of temporary blocks - which never
// reach the disk anyway - costs no writes at all.
func (s *BlockStore) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	if err := s.saveLocked(); err != nil {
		log.Printf("Nie udało się zapisać blokad: %v", err)
	}
}
