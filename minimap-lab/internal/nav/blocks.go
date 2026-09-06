package nav

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

	"minimap-lab/internal/mapdata"
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
	//
	// 2000 is worth about twenty ordinary steps: enough that any realistic way
	// around a shop counter wins, while a detour longer than that - which in
	// practice means there is no way around - still lets the route through.
	tempPenalty = 2000
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
	From           mapdata.Position `json:"from"`
	To             mapdata.Position `json:"to"`
	Outcome        string           `json:"outcome"`
	StillFrames    int              `json:"still_frames"`
	LastFrameAgeMS int              `json:"last_frame_age_ms"`
	// MovedSince says whether any step succeeded since the previous failure.
	// It is what separates "the bot walked away, came back and hit the same
	// tile again" - two encounters, which is what terrain looks like - from
	// "the bot has been standing in front of the same idle player for a
	// minute". Only the first may promote a block to permanent.
	MovedSince bool `json:"moved_since"`
}

// Decision is the server's answer. Result is ignored, temp, promoted or
// cleared; Reason always explains it, so a refused observation never looks
// like a dropped request in the panel.
type Decision struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
	// Revision is the overlay revision after this observation was applied. The
	// panel keeps the highest one it has seen and discards any route whose
	// overlay_revision is older - otherwise a path request already in flight
	// when a block is learned reinstalls the pre-block route on arrival, and
	// the bot walks straight back into the tile it just learned about.
	Revision uint64 `json:"revision"`
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
	// Edges carry an expiry and nothing else: they never promote, never reach
	// the disk and have no episodes to count.
	edges map[edgeKey]time.Time
	rev   uint64
	// path and dirty back the on-disk file of permanent blocks. dirty is set
	// only by changes worth persisting, so a burst of temporary blocks never
	// rewrites the file.
	path  string
	dirty bool
	// writeMu serialises whole write cycles. mu alone is not enough: encoding
	// and writing are deliberately not under it, so two flushes could otherwise
	// interleave and the slower rename would land last, silently replacing a
	// newer file with an older snapshot.
	writeMu sync.Mutex
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
	return &BlockStore{now: now, tiles: map[tileKey]*Blockage{}, edges: map[edgeKey]time.Time{}}
}

func (s *BlockStore) Revision() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rev
}

// decide stamps every answer with the revision the store is at, read under the
// caller's lock.
func (s *BlockStore) decide(result, reason string) Decision {
	return Decision{Result: result, Reason: reason, Revision: s.rev}
}

func (s *BlockStore) Observe(obs Observation) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)

	switch obs.Outcome {
	case "entered":
		cleared := s.clearLocked(tileKey{obs.To.X, obs.To.Y, obs.To.Z})
		// A failed diagonal blocks the edge rather than the tile, so walking it
		// has to lift that edge - clearing the tile alone would leave the
		// crossing refused for the rest of its TTL.
		if k := (edgeKey{obs.From.X, obs.From.Y, obs.To.X, obs.To.Y, obs.From.Z}); !s.edges[k].IsZero() {
			delete(s.edges, k)
			cleared = true
		}
		if cleared {
			s.rev++
			return s.decide("cleared", "Postać weszła na kratkę; blokada usunięta.")
		}
		return s.decide("ignored", "Kratka nie była zablokowana.")
	case "no_motion":
	default:
		return s.decide("ignored", fmt.Sprintf("Nieznany wynik próby: %s", obs.Outcome))
	}

	if obs.From.Z != obs.To.Z {
		return s.decide("ignored", "Krok między piętrami nie jest dowodem przeszkody.")
	}
	dx, dy := abs(obs.To.X-obs.From.X), abs(obs.To.Y-obs.From.Y)
	if dx > 1 || dy > 1 || (dx == 0 && dy == 0) {
		return s.decide("ignored", "Kratki nie sąsiadują ze sobą.")
	}
	if obs.StillFrames < minStillFrames {
		return s.decide("ignored",
			fmt.Sprintf("Za mało klatek bez ruchu: %d, wymagane %d.", obs.StillFrames, minStillFrames))
	}
	if obs.LastFrameAgeMS > maxFrameAgeMS {
		return s.decide("ignored", fmt.Sprintf("Ostatnia klatka starsza niż %d ms.", maxFrameAgeMS))
	}

	// A diagonal step also fails when both orthogonal tiles at the corner are
	// blocked, and then the target itself is often perfectly walkable. Learning
	// tiles from diagonals is how a bot slowly carves working rooms out of its
	// own map, so a diagonal only ever blocks the edge it failed on.
	if dx == 1 && dy == 1 {
		k := edgeKey{obs.From.X, obs.From.Y, obs.To.X, obs.To.Y, obs.From.Z}
		s.edges[k] = now.Add(edgeTTL)
		s.rev++
		return s.decide("temp", "Skos nieprzejezdny; zablokowano samo przejście.")
	}

	k := tileKey{obs.To.X, obs.To.Y, obs.To.Z}
	b := s.tiles[k]
	if b == nil {
		s.tiles[k] = &Blockage{Kind: KindTemp, Episodes: 1, First: now,
			Expires: now.Add(tempTTL), Forget: now.Add(forgetAfter)}
		s.rev++
		return s.decide("temp", "Pierwszy epizod; blokada tymczasowa.")
	}
	if b.Kind == KindPerm {
		b.Forget = now.Add(forgetAfter)
		return s.decide("promoted", "Kratka była już blokadą trwałą.")
	}
	// Still inside the previous block's lifetime: this is the same episode
	// seen again (a retry, a recomputed route, a resent report), not a second
	// independent one.
	if now.Before(b.Expires) {
		// Extend it. Without this a bump at 58s and another at 61s would count
		// as two episodes, even though the obstacle never left.
		b.Expires = now.Add(tempTTL)
		b.Forget = now.Add(forgetAfter)
		return s.decide("temp", "Ten sam epizod; blokada tymczasowa przedłużona.")
	}
	// Time apart is not enough. An obstacle that simply stayed put - an idle
	// player in a doorway - would otherwise be promoted to a permanent wall by
	// the mere passage of a minute, and permanent blocks never expire.
	if !obs.MovedSince {
		// Kind must come back with the deadline. sweepLocked ran at the top of
		// this call with the same clock and set it to KindNone, so renewing the
		// dates alone would leave a block that steers nothing, shows nowhere,
		// and can never promote - every later bump would just push the deadline
		// out again.
		b.Kind = KindTemp
		b.Expires = now.Add(tempTTL)
		b.Forget = now.Add(forgetAfter)
		s.rev++
		return s.decide("temp", "Ta sama przeszkoda bez chodzenia w międzyczasie; blokada tymczasowa odnowiona.")
	}
	b.Episodes++
	b.Forget = now.Add(forgetAfter)
	b.Kind, b.Expires = KindPerm, time.Time{}
	s.dirty = true
	s.rev++
	return s.decide("promoted", "Drugi niezależny epizod; blokada trwała.")
}

// Clear removes whatever is known about a tile. Reported presence beats any
// learned hypothesis, so this also drops the episode count - otherwise the
// next single bump would promote straight to permanent.
func (s *BlockStore) Clear(p mapdata.Position) bool {
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

// Snapshot is SnapshotAt without the revision, for callers that only need the
// overlay itself.
func (s *BlockStore) Snapshot(area image.Rectangle, z int) *Overlay {
	o, _ := s.SnapshotAt(area, z)
	return o
}

// SnapshotAt returns the overlay together with the revision it was taken at,
// both under one lock: read separately, the revision could describe a state
// the overlay never had.
func (s *BlockStore) SnapshotAt(area image.Rectangle, z int) (*Overlay, uint64) {
	if s == nil {
		return nil, 0
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
	return o, s.rev
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
	for k, expires := range s.edges {
		if !now.Before(expires) {
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
// Save serialises under the lock and writes outside it. Holding the mutex
// across fsync and rename would stall every route query and preview request
// for the duration of a disk write.
func (s *BlockStore) Save() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	path, data, err := s.encodeLocked()
	at := s.rev
	s.mu.Unlock()
	if err != nil || path == "" {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	s.mu.Lock()
	// Only if nothing changed while the file was being written. Clearing the
	// flag unconditionally would mark a promotion that arrived mid-write as
	// saved, and it would then be lost on restart - the one thing a permanent
	// block must survive.
	if s.rev == at {
		s.dirty = false
	}
	s.mu.Unlock()
	return nil
}

func (s *BlockStore) encodeLocked() (string, []byte, error) {
	if s.path == "" {
		return "", nil, nil
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
		return "", nil, err
	}
	return s.path, data, nil
}

// writeFileAtomic goes through a temporary in the same directory followed by a
// rename, so an interrupted write cannot leave a half-written file behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blocks-*.tmp")
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
	return os.Rename(name, path)
}

// Flush writes the file if a permanent block changed since the last write. The
// caller is the HTTP handler, so a burst of temporary blocks - which never
// reach the disk anyway - costs no writes at all.
func (s *BlockStore) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	dirty := s.dirty
	s.mu.Unlock()
	if !dirty {
		return
	}
	if err := s.Save(); err != nil {
		log.Printf("Nie udało się zapisać blokad: %v", err)
	}
}

type BlockInfo struct {
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Z           int    `json:"z"`
	Kind        string `json:"kind"`
	Episodes    int    `json:"episodes"`
	ExpiresInMS int    `json:"expires_in_ms"`
}

// List reports the learned blockages inside an area, for the panel's own view.
func (s *BlockStore) List(area image.Rectangle, z int) []BlockInfo {
	if s == nil {
		return []BlockInfo{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)
	out := []BlockInfo{}
	for k, b := range s.tiles {
		if b.Kind == KindNone || k[2] != z || !image.Pt(k[0], k[1]).In(area) {
			continue
		}
		info := BlockInfo{X: k[0], Y: k[1], Z: k[2], Episodes: b.Episodes, Kind: "perm"}
		if b.Kind == KindTemp {
			info.Kind = "temp"
			info.ExpiresInMS = int(b.Expires.Sub(now) / time.Millisecond)
		}
		out = append(out, info)
	}
	return out
}
