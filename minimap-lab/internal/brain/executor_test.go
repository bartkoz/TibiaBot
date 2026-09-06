package brain

import (
	"testing"
	"time"

	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/route"
)

func walkOut(direction string, next [2]int) *Output {
	return &Output{Action: ActionWalk, Direction: direction, Next: next}
}

func walkN() *Output { return walkOut("N", [2]int{100, 99}) }

func tile(x, y int, z ...int) *mapdata.Position {
	p := pos(x, y, z...)
	return &p
}

func mustIntent(t *testing.T, e *Executor, out *Output, now int) Intent {
	t.Helper()
	in, ok := e.IntentFor(out, at(now))
	if !ok {
		t.Fatalf("oczekiwano intencji w chwili %d", now)
	}
	return in
}

func refusesIntent(t *testing.T, e *Executor, out *Output, now int, why string) {
	t.Helper()
	if _, ok := e.IntentFor(out, at(now)); ok {
		t.Errorf("%s: wysłano intencję w chwili %d", why, now)
	}
}

func TestPierwszyKrokJestWysylanyOdRazu(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	got := mustIntent(t, e, walkN(), 0)
	if got.Action != "walk" || got.Direction != "N" {
		t.Errorf("intencja = %+v", got)
	}
}

func TestDrugiKrokNieIdzieDopokiPierwszyNieJestPotwierdzony(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(10), e.State().StepID)
	refusesIntent(t, e, walkN(), 20, "krok w toku")
}

func TestKlatkaSprzedEmisjiNieJestDowodemWykonaniaKroku(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(100), e.State().StepID)
	// Captured before the key was sent, even though it arrived after.
	e.Observe(tile(100, 99), at(50), at(120))
	if !e.State().Waiting {
		t.Error("klatka sprzed emisji zakończyła krok")
	}
}

func TestKlatkaPoEmisjiZDocelowaKratkaKonczyKrok(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(100), e.State().StepID)
	e.Observe(tile(100, 99), at(150), at(160))
	if e.State().Waiting {
		t.Error("krok nie został zakończony")
	}
	mustIntent(t, e, walkOut("N", [2]int{100, 98}), 170)
}

func TestBrakRuchuPrzedTimeoutemNiePowtarzaKroku(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: time.Second})
	e.Observe(tile(100, 100), at(0), at(0))
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(0), e.State().StepID)
	e.Observe(tile(100, 100), at(500), at(510))
	refusesIntent(t, e, walkN(), 900, "przed timeoutem")
}

func TestBrakRuchuPoTimeoucePowtarzaKrokRaz(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: time.Second})
	e.Observe(tile(100, 100), at(0), at(0))
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(0), e.State().StepID)
	e.Observe(tile(100, 100), at(500), at(510))
	retry := mustIntent(t, e, walkN(), 1100)
	if retry.Direction != "N" {
		t.Errorf("ponowienie = %+v", retry)
	}
	if got := e.State().Retries; got != 1 {
		t.Errorf("retries = %d, oczekiwano 1", got)
	}
}

func TestDrugaPorazkaTegoSamegoKrokuZglaszaBlokade(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: time.Second})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(0), e.State().StepID)
	mustIntent(t, e, walkN(), 1100)
	e.Emitted(at(1100), e.State().StepID)
	refusesIntent(t, e, walkN(), 2200, "druga porażka")
	if !e.State().Blocked {
		t.Error("druga porażka nie zgłosiła blokady")
	}
}

// The realistic scenario for a hard backstop: the route gets recomputed after
// each failure - a different target every time - and it still does not work.
// Cycles must keep counting across those replans; only a confirmed step
// resets them, never becoming blocked.
func TestTrzyRozneCeleNieudanePodRzadZatrzymujaWykonawce(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: 100 * time.Millisecond, MaxFailedCycles: 3})
	mustIntent(t, e, walkOut("N", [2]int{100, 99}), 0)
	e.Emitted(at(0), e.State().StepID)
	mustIntent(t, e, walkOut("N", [2]int{101, 99}), 200)
	e.Emitted(at(200), e.State().StepID)
	mustIntent(t, e, walkOut("N", [2]int{102, 99}), 400)
	e.Emitted(at(400), e.State().StepID)

	refusesIntent(t, e, walkOut("N", [2]int{102, 99}), 600, "trzecia porażka")
	if !e.State().Stopped {
		t.Fatal("wykonawca się nie zatrzymał")
	}
	refusesIntent(t, e, walkN(), 5000, "po zatrzymaniu")
}

// Every other test either omits the option or passes the default; this proves
// it actually takes effect rather than being ignored.
func TestMaxFailedCyclesInnyNizDomyslnyZatrzymujeWczesniej(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: 100 * time.Millisecond, MaxFailedCycles: 2})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(0), e.State().StepID)
	mustIntent(t, e, walkN(), 200)
	e.Emitted(at(200), e.State().StepID)
	refusesIntent(t, e, walkN(), 400, "drugi cykl przy limicie 2")
	if !e.State().Stopped {
		t.Error("limit 2 nie zatrzymał wykonawcy")
	}
}

func TestBlokadaTrzymaCelAleInnyCelJaCzysci(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: time.Second})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(0), e.State().StepID)
	mustIntent(t, e, walkN(), 1100)
	e.Emitted(at(1100), e.State().StepID)
	e.IntentFor(walkN(), at(2200))
	if !e.State().Blocked {
		t.Fatal("brak blokady po drugiej porażce")
	}
	refusesIntent(t, e, walkN(), 2300, "ten sam cel przy blokadzie")
	if !e.State().Blocked {
		t.Error("blokada zniknęła bez zmiany celu")
	}
	got := mustIntent(t, e, walkOut("N", [2]int{105, 99}), 2400)
	if got.Direction != "N" {
		t.Errorf("intencja dla nowego celu = %+v", got)
	}
	if e.State().Blocked {
		t.Error("blokada nie ustąpiła przy nowym celu")
	}
}

// Reproduces the bug where retries carried over across a replan: target A
// fails once, the follower moves on to a brand-new target B, and B's own first
// failure must not inherit A's already-spent retry.
func TestInnyCelPoJednejPorazceDostajeWlasnaSzanse(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: 100 * time.Millisecond})
	mustIntent(t, e, walkOut("N", [2]int{100, 99}), 0)
	e.Emitted(at(0), e.State().StepID)
	mustIntent(t, e, walkOut("N", [2]int{101, 99}), 200)
	e.Emitted(at(200), e.State().StepID)

	retry := mustIntent(t, e, walkOut("N", [2]int{101, 99}), 400)
	if retry.Direction != "N" {
		t.Errorf("ponowienie dla nowego celu = %+v", retry)
	}
	if e.State().Blocked {
		t.Error("nowy cel został zablokowany po pierwszej porażce")
	}
	if got := e.State().Retries; got != 1 {
		t.Errorf("retries = %d, oczekiwano 1", got)
	}
}

func TestNieznanaPozycjaNatychmiastWstrzymujeRuch(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(10), e.State().StepID)
	e.Observe(nil, at(100), at(110))
	if !e.State().Halted {
		t.Error("utrata pozycji nie wstrzymała wykonawcy")
	}
	refusesIntent(t, e, walkN(), 120, "bez znanej pozycji")
}

func TestPowrotPoprawnegoOdczytuOdblokowujeWykonawce(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(10), e.State().StepID)
	e.Observe(nil, at(100), at(110))
	e.Observe(tile(100, 100), at(200), at(210))
	if e.State().Halted {
		t.Error("wykonawca został wstrzymany mimo poprawnego odczytu")
	}
	mustIntent(t, e, walkN(), 220)
}

func TestNieoczekiwanaKratkaPorzucaKrokZamiastLiczycGoJakoNieudany(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(tile(100, 100), at(0), at(0))
	mustIntent(t, e, walkN(), 0)
	e.Emitted(at(10), e.State().StepID)
	// Pushed by a creature, or the player took over: not a failed step.
	e.Observe(tile(105, 120), at(100), at(110))
	if e.State().Waiting {
		t.Error("krok nie został porzucony")
	}
	if got := e.State().Retries; got != 0 {
		t.Errorf("retries = %d — przesunięcie postaci to nie jest nieudany krok", got)
	}
}

func stairsOut(next *route.Waypoint) *Output {
	return &Output{Action: ActionTransition, Index: 1,
		Waypoint:     &route.Waypoint{X: 100, Y: 100, Z: 7, Type: "stairs"},
		NextWaypoint: next}
}

func TestSchodySaPokonywaneKrokiemWStroneNastepnegoWaypointa(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(tile(100, 100, 7), at(0), at(0))
	// The stairs tile is on the current floor, so this is a walk. What makes
	// it a transition is that the proof is a changed floor, not a reached tile.
	got := mustIntent(t, e, stairsOut(&route.Waypoint{X: 101, Y: 100, Z: 6}), 10)
	if got.Action != "walk" || got.Direction != "E" {
		t.Errorf("intencja = %+v, oczekiwano kroku na wschód", got)
	}
	e.Emitted(at(20), e.State().StepID)
	e.Observe(tile(101, 100, 6), at(100), at(110))
	if e.State().Waiting {
		t.Error("zmiana piętra nie zakończyła kroku")
	}
	if e.State().ActionDone {
		t.Error("krok na schody zajął slot akcji w sterowniku")
	}
}

func TestSchodyBezNastepnegoWaypointaNieDajaKierunku(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(tile(100, 100, 7), at(0), at(0))
	refusesIntent(t, e, stairsOut(nil), 10, "brak następnika")
}

// Same guard, other half: next exists but there is no known tile to step from,
// so no direction can be computed either.
func TestSchodyBezWczesniejszejObserwacjiNieDajaKierunku(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	refusesIntent(t, e, stairsOut(&route.Waypoint{X: 101, Y: 100, Z: 6}), 0, "brak obserwacji")
}

// Recording a stairs waypoint and its landing point at the same x,y is normal
// for straight-up stairs. The direction is then empty - what the driver calls
// "nieznany kierunek" - and must never be sent; the human climbs these.
func TestSchodyZLadowaniemNaTejSamejKratceNieWysylajaPustegoKierunku(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})
	e.Observe(tile(100, 100, 7), at(0), at(0))
	refusesIntent(t, e, stairsOut(&route.Waypoint{X: 100, Y: 100, Z: 6}), 10, "pusty kierunek")
	if e.State().Waiting {
		t.Error("dla pustego kierunku został krok w toku")
	}
}

func ropeOut() *Output {
	return &Output{Action: ActionTransition, Index: 3,
		Waypoint: &route.Waypoint{X: 100, Y: 100, Z: 7, Type: "rope"}}
}

func TestAkcjaPietraCzekaNaZmianeZNieNaKratke(t *testing.T) {
	e := NewExecutor(ExecutorOptions{ActionTimeout: 5 * time.Second})
	got := mustIntent(t, e, ropeOut(), 0)
	if got.Action != "transition" || got.Type != "rope" || got.Waypoint != 3 {
		t.Errorf("intencja = %+v", got)
	}
	e.Emitted(at(10), e.State().StepID)
	e.Observe(tile(100, 100, 7), at(200), at(210))
	refusesIntent(t, e, ropeOut(), 220, "to samo piętro nie kończy akcji")
	e.Observe(tile(100, 100, 6), at(400), at(410))
	if e.State().Waiting {
		t.Error("zmiana piętra nie zakończyła akcji")
	}
	if !e.State().ActionDone {
		t.Error("akcja przez hotkey nie została oznaczona jako zakończona")
	}
}

func TestNieoczekiwanaKratkaPodczasAkcjiPietraPorzucaKrok(t *testing.T) {
	e := NewExecutor(ExecutorOptions{ActionTimeout: 5 * time.Second})
	e.Observe(tile(100, 100, 7), at(0), at(0))
	mustIntent(t, e, ropeOut(), 10)
	e.Emitted(at(20), e.State().StepID)
	// Pushed elsewhere on the same floor: no proof either way, but the
	// situation changed - this must not be charged as a failure.
	e.Observe(tile(120, 140, 7), at(100), at(110))
	if e.State().Waiting {
		t.Error("krok nie został porzucony")
	}
	if got := e.State().Retries; got != 0 {
		t.Errorf("retries = %d — przesunięcie postaci to nie jest nieudany krok", got)
	}
}

// Emitted() is never called here, simulating a rejected request: the caller
// never told the executor the key actually left the driver.
func TestKrokBezPotwierdzeniaEmisjiJestPorzucanyPoCzasie(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: 100 * time.Millisecond})
	mustIntent(t, e, walkN(), 0)
	if !e.State().AwaitingEmit {
		t.Error("stan nie mówi, że czekamy na potwierdzenie emisji")
	}
	refusesIntent(t, e, walkN(), 150, "wciąż w oknie łaski")
	retry := mustIntent(t, e, walkN(), 250)
	if retry.Direction != "N" {
		t.Errorf("ponowienie = %+v", retry)
	}
	if got := e.State().Retries; got != 1 {
		t.Errorf("retries = %d, oczekiwano 1", got)
	}
	if !e.State().AwaitingEmit {
		t.Error("ponowiony krok też czeka na potwierdzenie emisji")
	}
}

func TestSpoznionePotwierdzenieePorzuconegoKrokuNiePrzesuwaTerminu(t *testing.T) {
	e := NewExecutor(ExecutorOptions{StepTimeout: 100 * time.Millisecond})
	mustIntent(t, e, walkN(), 0)
	first := e.State().StepID
	// Grace period (twice the step timeout) expires: step one is dropped as a
	// failed cycle and step two, its retry, is sent in the same call.
	mustIntent(t, e, walkN(), 250)
	second := e.State().StepID
	if second == first {
		t.Fatal("identyfikator kroku został użyty ponownie")
	}
	e.Emitted(at(300), second)
	// Step one's late confirmation finally arrives, long after it was dropped.
	e.Emitted(at(310), first)

	// Had that stamped step two's emission at 310 instead of being ignored,
	// step two would still look in flight here (310 + 100 = 410 > 405). It
	// must instead time out from its real baseline of 300.
	e.IntentFor(walkN(), at(405))
	if !e.State().Blocked {
		t.Error("spóźnione potwierdzenie przesunęło termin następnego kroku")
	}
	if e.State().Waiting {
		t.Error("został krok w toku")
	}
}

// A step on mud or under paralysis takes well over a second; a shorter
// timeout would turn every such move into a false blockage.
func TestDomyslnyTimeoutKrokuWynosi1800ms(t *testing.T) {
	if got := NewExecutor(ExecutorOptions{}).StepTimeout(); got != 1800*time.Millisecond {
		t.Errorf("domyślny timeout = %v, oczekiwano 1.8s", got)
	}
}
