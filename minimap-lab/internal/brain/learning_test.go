package brain

import (
	"testing"
	"time"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/nav"
)

type failedStepOpts struct {
	frames  int
	from    mapdata.Position
	target  [2]int
	timeout int
	start   int
}

// failedStep drives one step that never moves the character, feeding readings
// that all show it still standing where it started.
func failedStep(e *Executor, o failedStepOpts) (nav.Observation, bool) {
	if o.frames == 0 {
		o.frames = 3
	}
	if o.from == (mapdata.Position{}) {
		o.from = pos(100, 100)
	}
	if o.target == [2]int{} {
		o.target = [2]int{100, 99}
	}
	if o.timeout == 0 {
		o.timeout = 1800
	}
	from := o.from
	e.Observe(&from, at(o.start), at(o.start))
	e.IntentFor(walkOut("N", o.target), at(o.start+10))
	e.Emitted(at(o.start+20), e.State().StepID)
	for i := 0; i < o.frames; i++ {
		e.Observe(&from, at(o.start+100+i*100), at(o.start+110+i*100))
	}
	e.IntentFor(walkOut("N", o.target), at(o.start+20+o.timeout+50))
	return e.TakeObservation()
}

func TestKrokBezRuchuZTrzemaKlatkamiProdukujeObserwacje(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	obs, ok := failedStep(e, failedStepOpts{})
	if !ok {
		t.Fatal("brak obserwacji po trzech nieruchomych klatkach")
	}
	if obs.Outcome != "no_motion" {
		t.Errorf("outcome = %q", obs.Outcome)
	}
	if obs.From != pos(100, 100) || obs.To != pos(100, 99) {
		t.Errorf("obserwacja = %+v", obs)
	}
	if obs.StillFrames < minStillFrames {
		t.Errorf("still_frames = %d, oczekiwano co najmniej %d", obs.StillFrames, minStillFrames)
	}
}

// One failure must be reported exactly once no matter how many times the loop
// ticks before the report goes out.
func TestObserwacjaJestOddawanaTylkoRaz(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	failedStep(e, failedStepOpts{})
	if _, ok := e.TakeObservation(); ok {
		t.Error("obserwacja została oddana drugi raz")
	}
}

// The key may never have left the driver, so nothing is known about the tile.
// Three readings, so only the missing confirmation can reject this.
func TestKrokBezPotwierdzeniaEmisjiNiczegoNieUczy(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	from := pos(100, 100)
	e.Observe(&from, at(0), at(0))
	e.IntentFor(walkN(), at(10))
	for _, ms := range []int{100, 200, 300} {
		e.Observe(&from, at(ms), at(ms+10))
	}
	e.IntentFor(walkN(), at(10+2*1800+50))
	if _, ok := e.TakeObservation(); ok {
		t.Error("krok bez potwierdzenia emisji czegoś nauczył")
	}
}

// Walking onto stairs changes Z; that is not a wall. Three readings, so only
// the floor check can reject this.
func TestZmianaPietraNiczegoNieUczy(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	start := pos(100, 100, 7)
	e.Observe(&start, at(0), at(0))
	e.IntentFor(walkN(), at(10))
	e.Emitted(at(20), e.State().StepID)
	lower := pos(100, 100, 6)
	for _, ms := range []int{100, 200, 300} {
		e.Observe(&lower, at(ms), at(ms+10))
	}
	e.IntentFor(walkN(), at(20+1850))
	if _, ok := e.TakeObservation(); ok {
		t.Error("zmiana piętra czegoś nauczyła")
	}
}

func TestPrzesuniecieGdzieIndziejNiczegoNieUczy(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	start := pos(100, 100)
	e.Observe(&start, at(0), at(0))
	e.IntentFor(walkN(), at(10))
	e.Emitted(at(20), e.State().StepID)
	// Pushed by a creature, or the player took over.
	e.Observe(tile(101, 101), at(100), at(110))
	e.IntentFor(walkN(), at(20+1850))
	if _, ok := e.TakeObservation(); ok {
		t.Error("przesunięcie postaci czegoś nauczyło")
	}
}

func TestZaMaloKlatekToZaSlabyDowod(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	if _, ok := failedStep(e, failedStepOpts{frames: 1}); ok {
		t.Error("jedna klatka wystarczyła za dowód")
	}
}

func TestUdanyKrokNieProdukujeObserwacji(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	start := pos(100, 100)
	e.Observe(&start, at(0), at(0))
	e.IntentFor(walkN(), at(10))
	e.Emitted(at(20), e.State().StepID)
	e.Observe(tile(100, 99), at(100), at(110))
	if _, ok := e.TakeObservation(); ok {
		t.Error("udany krok wyprodukował obserwację")
	}
}

// 300 ms after the timeout the character finally arrives: that was lag.
func TestSpoznioneWejscieNaKratkeOdwolujeNauke(t *testing.T) {
	e := NewExecutor(ExecutorOptions{LateArrival: 600 * time.Millisecond})
	failedStep(e, failedStepOpts{})
	e.Observe(tile(100, 99), at(2100), at(2170))
	obs, ok := e.TakeObservation()
	if !ok {
		t.Fatal("brak odwołania nauki")
	}
	if obs.Outcome != "entered" || obs.To != pos(100, 99) {
		t.Errorf("obserwacja = %+v", obs)
	}
}

func TestWejscieDlugoPoTimeoucieNieJestJuzOdwolaniem(t *testing.T) {
	e := NewExecutor(ExecutorOptions{LateArrival: 600 * time.Millisecond})
	failedStep(e, failedStepOpts{})
	e.TakeObservation()
	e.Observe(tile(100, 99), at(5000), at(5010))
	if _, ok := e.TakeObservation(); ok {
		t.Error("wejście długo po timeoucie zostało uznane za odwołanie")
	}
}

func TestSpoznioneWejscieZdejmujeBlokadeCelu(t *testing.T) {
	e := NewExecutor(ExecutorOptions{LateArrival: 600 * time.Millisecond})
	failedStep(e, failedStepOpts{})
	failedStep(e, failedStepOpts{start: 3000})
	if !e.State().Blocked {
		t.Fatal("druga porażka nie zablokowała celu")
	}
	e.Observe(tile(100, 99), at(5000), at(5100))
	if e.State().Blocked {
		t.Error("kratka, na którą postać weszła, pozostała zablokowana")
	}
}

// The follower keeps asking for the same tile - with a cost penalty rather
// than a wall on the server, A* may well still route through it. Without a TTL
// the executor would refuse that target for the rest of the session.
func TestBlokadaCeluWygasaPoSwoimCzasie(t *testing.T) {
	e := NewExecutor(ExecutorOptions{BlockedTTL: 60 * time.Second, MaxFailedCycles: 99})
	failedStep(e, failedStepOpts{})
	failedStep(e, failedStepOpts{start: 3000})
	if !e.State().Blocked {
		t.Fatal("brak blokady")
	}
	if _, ok := e.IntentFor(walkN(), at(20000)); ok {
		t.Error("blokada ustąpiła przed czasem")
	}
	if _, ok := e.IntentFor(walkN(), at(200000)); !ok {
		t.Error("blokada nie wygasła po swoim czasie")
	}
}

// The failure that ends the first episode leaves cycles at two. If the lapse
// of the block does not clear that count, the very next attempt - the one that
// would teach the permanent block - trips maxFailedCycles and stops the bot
// for good, exactly when it was about to learn something useful.
func TestWygasnieceBlokadyDajeBotowiNowaSzanseNieZatrzymanie(t *testing.T) {
	e := NewExecutor(ExecutorOptions{BlockedTTL: 60 * time.Second})
	failedStep(e, failedStepOpts{})
	failedStep(e, failedStepOpts{start: 3000})
	if !e.State().Blocked {
		t.Fatal("brak blokady")
	}
	e.IntentFor(walkN(), at(100000))
	failedStep(e, failedStepOpts{start: 110000})
	if e.State().Stopped {
		t.Error("bot zatrzymał się w chwili, w której uczył się blokady trwałej")
	}
}

// A late arrival is proof the step worked. Three unrelated lag spikes in a
// session must not add up to a permanent stop.
func TestSpoznioneWejscieKasujeLicznikNieudanychCykli(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	failedStep(e, failedStepOpts{})
	e.Observe(tile(100, 99), at(2100), at(2170))
	if got := e.State().Cycles; got != 0 {
		t.Errorf("cycles = %d — krok, który się jednak udał, nadal liczy się jako porażka", got)
	}
}

// Only "the bot walked away, came back and hit the same tile again" looks like
// terrain. "The bot has been standing in front of the same idle player for a
// minute" does not, and the server needs to be able to tell them apart.
func TestObserwacjaNiesieInformacjeCzyPostacChodzila(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	obs, ok := failedStep(e, failedStepOpts{})
	if !ok || obs.MovedSince {
		t.Errorf("stanie w miejscu zgłoszone jako chodzenie: %+v ok=%v", obs, ok)
	}
	start := pos(100, 100)
	e.Observe(&start, at(4000), at(4000))
	e.IntentFor(walkN(), at(4010))
	e.Emitted(at(4020), e.State().StepID)
	e.Observe(tile(100, 99), at(4100), at(4110))
	obs, ok = failedStep(e, failedStepOpts{from: pos(100, 99), target: [2]int{100, 98}, start: 5000})
	if !ok || !obs.MovedSince {
		t.Errorf("udany krok nie odnotował chodzenia: %+v ok=%v", obs, ok)
	}
}

// The server needs from to drop the edge a failed diagonal blocked; to alone
// does not identify an edge.
func TestSpoznioneWejscieNiesieKratkeStartowa(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	failedStep(e, failedStepOpts{})
	e.Observe(tile(100, 99), at(2100), at(2170))
	obs, ok := e.TakeObservation()
	if !ok {
		t.Fatal("brak obserwacji")
	}
	if obs.From != pos(100, 100) || obs.To != pos(100, 99) {
		t.Errorf("obserwacja = %+v", obs)
	}
}

// The frame documents an arrival inside the window but reaches the loop much
// later. Judging by the processing time would throw away a genuine revocation.
func TestWejscieOcenianeJestPoCzasieKlatkiNiePrzetworzenia(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	failedStep(e, failedStepOpts{})
	const failedAt = 1870
	e.Observe(tile(100, 99), at(failedAt+1900), at(failedAt+2800))
	obs, ok := e.TakeObservation()
	if !ok || obs.Outcome != "entered" {
		t.Errorf("obserwacja = %+v ok=%v, oczekiwano entered", obs, ok)
	}
}
