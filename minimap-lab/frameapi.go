package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"strconv"
	"time"

	"minimap-lab/internal/brain"
	"minimap-lab/internal/frame"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/route"
)

// loopReady refuses requests until the processing loop is available.
func (s *server) loopReady(w http.ResponseWriter) bool {
	if s.loop == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"Pętla odczytu jest niedostępna. Uruchom panel ponownie.")
		return false
	}
	return true
}

// Capture permission and movement permission are separate. Starting a new
// screen session never arms the keyboard driver.
func (s *server) startCapture(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	session, err := newCaptureSession()
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return
	}
	if s.driver != nil {
		s.driver.Disarm("nowa sesja przechwytywania")
	}
	s.sessionMu.Lock()
	s.session = session
	s.sessionMu.Unlock()
	s.loop.ResetCapture(r.Context(), session)
	writeJSON(w, map[string]any{"session": strconv.FormatUint(session, 10)})
}

func newCaptureSession() (uint64, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	// Zero is reserved for "no session", so a client that forgot to stamp its
	// frames cannot accidentally match.
	v := binary.LittleEndian.Uint64(buf[:])
	if v == 0 {
		v = 1
	}
	return v, nil
}

func (s *server) arm(w http.ResponseWriter, r *http.Request) {
	if s.driver == nil {
		writeJSONError(w, http.StatusServiceUnavailable,
			"Sterowanie wyłączone. Uruchom panel z -input dry albo -input system.")
		return
	}
	state, err := s.driver.Arm()
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	session := s.captureSession()
	if session == 0 {
		session, err = newCaptureSession()
	}
	if err != nil {
		s.driver.Disarm("nie udało się wylosować tokenu sesji")
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.sessionMu.Lock()
	s.session = session
	s.sessionMu.Unlock()
	// The session travels as a decimal string, not a number: it is a uint64,
	// and JSON numbers lose precision above 2^53 in every browser. Rounded on
	// the way in, it would never match again and every frame would be refused.
	writeJSON(w, map[string]any{"armed": state.Armed, "target": state.Target,
		"session": strconv.FormatUint(session, 10)})
}

func (s *server) disarm(w http.ResponseWriter, r *http.Request) {
	if s.driver != nil {
		s.driver.Disarm("zatrzymane z panelu")
	}
	s.sessionMu.Lock()
	s.session = 0
	s.sessionMu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *server) captureSession() uint64 {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	return s.session
}

// readBody reads a bounded request body into a slice the caller owns.
//
// Deliberately not pooled: frame.Parse hands the loop regions that point into
// this buffer, and the loop reads them after the handler has returned. A
// recycled buffer would be overwritten by the next frame mid-match. One
// allocation of a few tens of kilobytes per frame is a price worth paying for
// not having to reason about that.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *server) frame(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	received := time.Now()
	data, err := readBody(w, r, frame.MaxBody)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Klatka jest za duża albo nie dotarła w całości.")
		return
	}
	f, err := frame.Parse(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A reload leaves the previous stream's frames in flight; believing them
	// would feed the brain pictures from a capture the user has ended.
	if want := s.captureSession(); want == 0 || f.Session != want {
		writeJSONError(w, http.StatusForbidden, "Klatka pochodzi z innej sesji przechwytywania. Włącz śledzenie ponownie.")
		return
	}
	// The handler never waits for the match: the answer is whatever the loop
	// knows right now, carrying the frame number it has actually finished with.
	s.loop.Submit(f, received)
	writeJSON(w, s.loop.Snapshot())
}

func (s *server) state(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	writeJSON(w, s.loop.Snapshot())
}

// configRequest is the whole panel-adjustable surface in one document. It is
// validated before anything is written, so one bad field never partially
// applies - the rule the input config already followed, kept.
type configRequest struct {
	Brain            brain.Config      `json:"brain"`
	Keys             map[string]string `json:"keys"`
	ClickAfterHotkey bool              `json:"click_after_hotkey"`
	Directions       map[string]string `json:"directions"`
	Tile             *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"tile,omitempty"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(v); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Nieprawidłowe żądanie JSON")
		return false
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "Treść żądania musi zawierać dokładnie jeden dokument JSON.")
		return false
	}
	return true
}

func (s *server) config(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	var body configRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	// A request that omits "fight" keeps the retained config (SetConfig's own
	// fightAbsent handling below) rather than the zero value sitting in
	// body.Brain.Fight - the collision check must see whichever one is about
	// to actually apply, or a key already claimed by an attack/spell binding
	// made through the API is invisible to every panel request, since the
	// panel has no fight module yet and so never sends the key at all.
	effectiveFight := body.Brain.Fight
	if body.Brain.FightAbsent() {
		effectiveFight = s.loop.CurrentFightConfig(r.Context())
	}
	if err := keyConflicts(body, effectiveFight); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Everything is checked before anything is stored. The tile goes first
	// because it is the only part that cannot be rolled back cheaply.
	if body.Tile != nil && s.driver != nil {
		if err := s.driver.Calibrate(body.Tile.X, body.Tile.Y); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if s.driver != nil {
		if err := s.driver.SetInputConfig(body.Keys, body.ClickAfterHotkey, body.Directions); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.loop.SetConfig(r.Context(), body.Brain); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// actionNames are the floor actions in the genitive, for a sentence that reads
// like Polish rather than like a field name.
var actionNames = map[string]string{
	"rope": "liny", "ladder": "drabiny", "hole": "dziury", "shovel": "łopaty",
}

// keyCategory is one group of bindings for the collision check. Duplicates
// are legal inside a category and never across one, so the check needs both
// the machine-readable group and the phrase a refusal should name.
type keyCategory struct {
	group  string
	phrase string
}

// keyConflicts refuses a request where one key would do two different jobs.
// The halves of the config are applied to different owners - the action
// hotkeys to the driver, the rules to the loop - so this is the only point
// where all of them are visible at once, and it runs before anything is
// stored.
//
// fight is the fight config to check against, not necessarily body.Brain.Fight:
// a request that omits "fight" (every panel request today, since the panel
// has no fight module yet) keeps the retained config rather than applying a
// zero one, and this check must see whichever one is about to actually
// apply - the caller resolves that before calling in.
//
// The attack key and every spell hotkey form one category, inside which
// duplicates are deliberately allowed: fight.Engine tracks one cooldown per
// hotkey, shared by every rule that names it, so two rules on one key behave
// exactly the way the user expects. Across categories they do not.
// heal.Engine and fight.Engine keep independent cooldown maps, so a key bound
// to both a heal rule and a spell would be pressed twice in quick succession
// with neither engine aware of the other's emission - which is the failure
// this check exists to prevent, not merely a tidiness rule.
//
// Floor actions are claimed first so that a collision with one is reported by
// the genitive name a human recognises ("liny") rather than by a rule number.
func keyConflicts(body configRequest, fight brain.FightConfig) error {
	held := map[string]keyCategory{}
	// claim records a key for a category, or reports who has it already.
	claim := func(key string, as keyCategory) (keyCategory, bool) {
		if key == "" {
			return keyCategory{}, true
		}
		if had, ok := held[key]; ok && had.group != as.group {
			return had, false
		}
		held[key] = as
		return keyCategory{}, true
	}
	for action, key := range body.Keys {
		name, ok := actionNames[action]
		if !ok {
			name = action
		}
		if had, ok := claim(key, keyCategory{group: "action", phrase: name}); !ok {
			return fmt.Errorf("klawisz %s obsługuje %s, a jest przypisany już do %s", key, name, had.phrase)
		}
	}
	for i, r := range body.Brain.Heal.Rules {
		n := i + 1
		as := keyCategory{group: "heal", phrase: fmt.Sprintf("reguły leczenia %d", n)}
		if had, ok := claim(r.Hotkey, as); !ok {
			return fmt.Errorf("reguła %d używa klawisza %s, przypisanego już do %s", n, r.Hotkey, had.phrase)
		}
	}
	const fightGroup = "fight"
	if had, ok := claim(fight.AttackKey,
		keyCategory{group: fightGroup, phrase: "ataku i czarów"}); !ok {
		return fmt.Errorf("klawisz ataku %s jest przypisany już do %s", fight.AttackKey, had.phrase)
	}
	for i, r := range fight.Spells {
		n := i + 1
		as := keyCategory{group: fightGroup, phrase: "ataku i czarów"}
		if had, ok := claim(r.Hotkey, as); !ok {
			return fmt.Errorf("reguła czaru %d używa klawisza %s, przypisanego już do %s", n, r.Hotkey, had.phrase)
		}
	}
	return nil
}

func (s *server) putRoute(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	data, err := readBody(w, r, 1<<20)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "Plik trasy jest za duży.")
		return
	}
	parsed, err := route.Parse(data)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.loop.SetRoute(r.Context(), parsed)
	writeJSON(w, map[string]any{"ok": true, "waypoints": len(parsed.Waypoints)})
}

// getRoute hands back the route including whatever the recorder added, which
// is how the panel writes a recorded route to a file. Waypoints never ride in
// the snapshot, so this is the only way to get at them.
func (s *server) getRoute(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	data, err := route.Serialize(s.loop.Route(r.Context()))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *server) addWaypoint(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	if !s.loop.AddManualWaypoint(r.Context()) {
		writeJSONError(w, http.StatusConflict, "Brak znanej pozycji albo trasa osiągnęła limit punktów.")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// preview cuts the neighbourhood picture out of the atlas. It used to ride
// inside every match reply as a data URI; the snapshot has to stay small, so
// the panel fetches it separately when preview_revision says it changed.
func (s *server) preview(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	snap := s.loop.Snapshot()
	if snap == nil || snap.Position == nil {
		writeJSONError(w, http.StatusNotFound, "Pozycja nieznana.")
		return
	}
	p := *snap.Position
	area := image.Rect(p.X-previewHalf, p.Y-previewHalf, p.X+previewHalf+1, p.Y+previewHalf+1)
	atlas, err := s.locator.AtlasAround(p.Z, area)
	if err != nil || atlas == nil {
		writeJSONError(w, http.StatusNotFound, "Brak danych mapy dla tej okolicy.")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(previewPNG(atlas, p.X, p.Y))
}

const previewHalf = 64

var _ = mapdata.Position{}
