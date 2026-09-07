package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"minimap-lab/internal/brain"
	"minimap-lab/internal/frame"
	"minimap-lab/internal/input"
	"minimap-lab/internal/locate"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/testenv"
)

// slowLocator parks inside a match until the test releases it, and announces
// on entered that a match really is in flight. Both are channels rather than
// counters so the test never reads state the loop goroutine is writing.
type slowLocator struct {
	gate    chan struct{}
	entered chan struct{}
}

func (l *slowLocator) Locate(context.Context, image.Image, locate.Request) (locate.Result, *mapdata.Atlas, error) {
	if l.entered != nil {
		select {
		case l.entered <- struct{}{}:
		default:
		}
	}
	if l.gate != nil {
		<-l.gate
	}
	return locate.Result{Found: false, Mode: "local", Reason: "atrapa"}, nil, nil
}

type brainFixture struct {
	server  *server
	session uint64
	cancel  context.CancelFunc
	seq     uint64
	videoUS uint64
}

func brainServer(t *testing.T, locator brain.Locator) *brainFixture {
	t.Helper()
	s := newServer(t.TempDir())
	em, err := input.SelectEmitter("dry")
	if err != nil {
		t.Fatal(err)
	}
	s.driver = input.NewDriver(em, input.DefaultMaxObservationAgeMS)
	if locator == nil {
		locator = &slowLocator{}
	}
	s.loop = brain.NewLoop(brain.Deps{
		Locator: locator, Planner: s.planner, Blocks: s.blocks,
		Driver: s.driver, Now: time.Now,
	})
	ctx, cancel := context.WithCancel(context.Background())
	go s.loop.Run(ctx)
	t.Cleanup(cancel)

	f := &brainFixture{server: s, cancel: cancel}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8095/api/arm", nil))
	if w.Code != 200 {
		t.Fatalf("arm: %d %s", w.Code, w.Body.String())
	}
	var armed struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &armed); err != nil {
		t.Fatal(err)
	}
	session, err := strconv.ParseUint(armed.Session, 10, 64)
	if err != nil || session == 0 {
		t.Fatalf("uzbrojenie nie zwróciło tokenu sesji: %q (%v)", armed.Session, err)
	}
	f.session = session
	return f
}

func (f *brainFixture) body(session uint64) []byte {
	f.seq++
	f.videoUS += 100_000
	body := make([]byte, frame.HeaderSize+frame.RegionHeader)
	copy(body[0:4], frame.Magic)
	body[4], body[5] = frame.FormatVersion, 1
	binary.LittleEndian.PutUint64(body[8:], session)
	binary.LittleEndian.PutUint64(body[16:], f.seq)
	binary.LittleEndian.PutUint64(body[24:], f.videoUS)
	hdr := body[frame.HeaderSize:]
	hdr[0] = byte(frame.RegionMinimap)
	binary.LittleEndian.PutUint16(hdr[4:], 2)
	binary.LittleEndian.PutUint16(hdr[6:], 2)
	binary.LittleEndian.PutUint32(hdr[8:], 16)
	return append(body, make([]byte, 16)...)
}

func (f *brainFixture) post(t *testing.T, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "POST", path, body)
}

func (f *brainFixture) request(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:8095"+path, bytes.NewReader(body))
	w := httptest.NewRecorder()
	f.server.routes().ServeHTTP(w, r)
	return w
}

// A reload leaves the previous stream's frames in flight; believing them would
// feed the brain pictures from a capture the user has ended.
func TestFrameFromAnotherCaptureSessionIsRefused(t *testing.T) {
	f := brainServer(t, nil)
	w := f.post(t, "/api/frame", f.body(f.session+1))
	if w.Code != http.StatusForbidden {
		t.Errorf("kod = %d, oczekiwano 403", w.Code)
	}
}

func TestFrameWithoutArmingIsRefused(t *testing.T) {
	f := brainServer(t, nil)
	f.post(t, "/api/disarm", nil)
	if w := f.post(t, "/api/frame", f.body(f.session)); w.Code != http.StatusForbidden {
		t.Errorf("kod = %d, oczekiwano 403 po rozbrojeniu", w.Code)
	}
}

// An unread body makes Go tear down the TCP connection instead of reusing it,
// and at twenty requests a second that exhausts ephemeral ports in minutes.
func TestFrameHandlerRefusesAnOversizedBody(t *testing.T) {
	f := brainServer(t, nil)
	huge := make([]byte, frame.MaxBody+1)
	copy(huge[0:4], frame.Magic)
	if w := f.post(t, "/api/frame", huge); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("kod = %d, oczekiwano 413", w.Code)
	}
}

func TestFrameHandlerRefusesAMalformedBody(t *testing.T) {
	f := brainServer(t, nil)
	if w := f.post(t, "/api/frame", []byte("nie klatka")); w.Code != http.StatusBadRequest {
		t.Errorf("kod = %d, oczekiwano 400", w.Code)
	}
}

// The handler must not block on matching: it takes the frame, answers with
// what the loop knows now, and says which frame that answer describes.
func TestFrameHandlerAnswersWithoutWaitingForTheMatch(t *testing.T) {
	slow := &slowLocator{gate: make(chan struct{}), entered: make(chan struct{}, 1)}
	f := brainServer(t, slow)

	first := f.post(t, "/api/frame", f.body(f.session))
	if first.Code != 200 {
		t.Fatalf("%d: %s", first.Code, first.Body.String())
	}
	// Wait until a match is genuinely parked, so the second POST is answered
	// while the loop is busy rather than merely before it started.
	select {
	case <-slow.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("pętla nie zaczęła dopasowania")
	}
	done := make(chan int, 1)
	go func() { done <- f.post(t, "/api/frame", f.body(f.session)).Code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("druga klatka: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler czekał na dopasowanie")
	}
	close(slow.gate)
}

func TestConfigIsRejectedWholeOrAppliedWhole(t *testing.T) {
	f := brainServer(t, nil)
	good := `{"brain":{"zoom":1,"min_score":0.85,"min_gap":0.015,"speed":20,
		"floor_radius":8,"record_every":10,"tolerance":1,"floor":7},
		"keys":{"rope":"f7"},"directions":{"N":"numpad8"}}`
	if w := f.request(t, "PUT", "/api/config", []byte(good)); w.Code != 200 {
		t.Fatalf("dobra konfiguracja odrzucona: %d %s", w.Code, w.Body.String())
	}
	bad := `{"brain":{"zoom":99,"min_score":0.85,"min_gap":0.015,"speed":20,
		"floor_radius":8,"record_every":10,"tolerance":1,"floor":7},
		"keys":{"rope":"nieistniejący"},"directions":{}}`
	w := f.request(t, "PUT", "/api/config", []byte(bad))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("kod = %d, oczekiwano 400", w.Code)
	}
	// One bad field must not silently clear an unrelated one.
	if got := f.server.driver.ActionKeys["rope"]; got != "f7" {
		t.Errorf("hotkey liny = %q — odrzucona konfiguracja jednak coś zmieniła", got)
	}
}

func TestRouteRoundTripsThroughTheAPI(t *testing.T) {
	f := brainServer(t, nil)
	in := `{"version":1,"name":"Venore","waypoints":[
		{"x":100,"y":200,"z":7,"type":"walk","label":""},
		{"x":110,"y":200,"z":7,"type":"rope","label":"lina"}]}`
	if w := f.request(t, "PUT", "/api/route", []byte(in)); w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	w := f.request(t, "GET", "/api/route", nil)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"rope"`) || !strings.Contains(w.Body.String(), `"lina"`) {
		t.Errorf("trasa nie wróciła w całości: %s", w.Body.String())
	}
}

func TestRouteIsRefusedWhenMalformed(t *testing.T) {
	f := brainServer(t, nil)
	if w := f.request(t, "PUT", "/api/route", []byte(`{"version":9,"waypoints":[]}`)); w.Code != http.StatusBadRequest {
		t.Errorf("kod = %d, oczekiwano 400", w.Code)
	}
}

// The snapshot rides on every single frame, so nothing unbounded may travel
// with it.
func TestSnapshotStaysSmall(t *testing.T) {
	f := brainServer(t, nil)
	points := make([]string, 1000)
	for i := range points {
		points[i] = `{"x":100,"y":200,"z":7}`
	}
	in := `{"version":1,"waypoints":[` + strings.Join(points, ",") + `]}`
	if w := f.request(t, "PUT", "/api/route", []byte(in)); w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	w := f.request(t, "GET", "/api/state", nil)
	if w.Code != 200 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() > 4096 {
		t.Errorf("snapshot ma %d bajtów — coś nieograniczonego jedzie razem z nim", w.Body.Len())
	}
	if !strings.Contains(w.Body.String(), `"count":1000`) {
		t.Error("snapshot nie mówi, ile punktów ma trasa")
	}
}

// Without an emitter the brain routes must say why, not fail as a nil panic.
func TestBrainRoutesAnswer503WithoutALoop(t *testing.T) {
	s := newServer(t.TempDir())
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/frame"}, {"GET", "/api/state"}, {"PUT", "/api/config"},
		{"PUT", "/api/route"}, {"GET", "/api/route"}, {"GET", "/api/preview"},
		{"POST", "/api/route/waypoint"},
	} {
		r := httptest.NewRequest(c.method, "http://127.0.0.1:8095"+c.path, bytes.NewReader(nil))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: kod = %d, oczekiwano 503", c.method, c.path, w.Code)
		}
	}
}

// The session is a uint64, and a JSON number would be rounded by every browser
// above 2^53 - after which no frame would ever match again.
func TestCaptureSessionTravelsAsAStringNotANumber(t *testing.T) {
	f := brainServer(t, nil)
	w := f.post(t, "/api/arm", nil)
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["session"].(string); !ok {
		t.Fatalf("session = %T, oczekiwano napisu", raw["session"])
	}
}

// The whole stack in one test: a real capture, the real matcher, the real
// loop, over HTTP. Everything below it is unit-tested, but only this says the
// pieces are actually wired to each other.
func TestFrameEndToEndLocatesTheCharacter(t *testing.T) {
	dir := t.TempDir()
	testenv.SavePNG(t, filepath.Join(dir, "Minimap_Color_32768_32000_7.png"),
		testenv.LoadFixture(t, "venore-reference.png"))

	s := newServer(dir)
	em, err := input.SelectEmitter("dry")
	if err != nil {
		t.Fatal(err)
	}
	s.driver = input.NewDriver(em, input.DefaultMaxObservationAgeMS)
	s.loop = brain.NewLoop(brain.Deps{
		Locator: s.locator, Planner: s.planner, Blocks: s.blocks,
		Driver: s.driver, Tile: s.tileVerdict, Now: time.Now,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.loop.Run(ctx)

	f := &brainFixture{server: s}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1:8095/api/arm", nil))
	var armed struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &armed); err != nil {
		t.Fatal(err)
	}
	f.session, err = strconv.ParseUint(armed.Session, 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	c := testenv.VenoreCalibration()
	cfg := fmt.Sprintf(`{"brain":{"zoom":%d,"marker_x":%d,"marker_y":%d,"mask_radius":%d,
		"min_score":%v,"min_gap":%v,"floor":7,"speed":20,"floor_radius":8,
		"record_every":10,"tolerance":1}}`, c.Zoom, c.MarkerX, c.MarkerY, c.MaskRadius, c.MinScore, c.MinGap)
	if w := f.request(t, "PUT", "/api/config", []byte(cfg)); w.Code != 200 {
		t.Fatalf("konfiguracja: %d %s", w.Code, w.Body.String())
	}

	capture := testenv.LoadFixture(t, "venore-capture.png")
	b := capture.Bounds()
	im := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(im, im.Bounds(), capture, b.Min, draw.Src)

	deadline := time.Now().Add(30 * time.Second)
	for {
		if w := f.post(t, "/api/frame", f.frameWith(im, b.Dx(), b.Dy())); w.Code != 200 {
			t.Fatalf("klatka: %d %s", w.Code, w.Body.String())
		}
		var state brain.State
		if err := json.Unmarshal(f.request(t, "GET", "/api/state", nil).Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.Position != nil {
			want := mapdata.Position{X: 32958, Y: 32077, Z: 7}
			if *state.Position != want {
				t.Fatalf("pozycja = %+v, oczekiwano %+v", *state.Position, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("mózg nie ustalił pozycji z prawdziwego zrzutu minimapy")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// frameWith wraps real pixels in the binary body the panel would send.
func (f *brainFixture) frameWith(im *image.NRGBA, w, h int) []byte {
	f.seq++
	f.videoUS += 100_000
	body := make([]byte, frame.HeaderSize+frame.RegionHeader)
	copy(body[0:4], frame.Magic)
	body[4], body[5] = frame.FormatVersion, 1
	binary.LittleEndian.PutUint64(body[8:], f.session)
	binary.LittleEndian.PutUint64(body[16:], f.seq)
	binary.LittleEndian.PutUint64(body[24:], f.videoUS)
	hdr := body[frame.HeaderSize:]
	hdr[0] = byte(frame.RegionMinimap)
	binary.LittleEndian.PutUint16(hdr[4:], uint16(w))
	binary.LittleEndian.PutUint16(hdr[6:], uint16(h))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(len(im.Pix)))
	return append(body, im.Pix...)
}
