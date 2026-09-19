package input

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// driverAt builds an armed driver with a controllable clock, so timeouts are
// tested without sleeping.
func driverAt(t *testing.T, start time.Time) (*Driver, *DryEmitter, *time.Time) {
	t.Helper()
	now := start
	em := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	d.now = func() time.Time { return now }
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	return d, em, &now
}

// fresh is an observation age comfortably inside every threshold under test.
const fresh = 100 * time.Millisecond

// failingEmitter wraps a DryEmitter but fails every TapKey call, so a test can
// exercise the driver's OS-failure path without touching input.go.
type failingEmitter struct {
	*DryEmitter
	err error
}

func (e *failingEmitter) TapKey(key string, holdMS int) error {
	return e.err
}

func TestDriverRefusesEverythingWhileDisarmed(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 42}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)

	got := d.Walk("N", fresh)

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("a disarmed driver must not touch the system")
	}
}

func TestDriverEmitsOneTapPerAcceptedIntent(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Walk("N", fresh)

	if got.Status != "emitted" || got.Key != "numpad8" {
		t.Fatalf("got %+v", got)
	}
	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap numpad8 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverRefusesStaleObservation(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	got := d.Walk("N", time.Duration(DefaultMaxObservationAgeMS+1)*time.Millisecond)

	if got.Status != "refused" || got.Reason == "" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("walking on a stale position is the worst possible state")
	}
}

func TestDriverUsesConfiguredMaxObservationAge(t *testing.T) {
	// A slow capture-to-reply round trip (the panel's "Cały odczyt" telemetry)
	// on a real machine could exceed the 400ms default and refuse every step;
	// -stale-ms must actually reach the driver, not just exist as a flag.
	em := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	d := NewDriver(em, 1000)
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	// 600 ms is stale under the 400 ms default and fresh under 1000 ms.
	got := d.Walk("N", 600*time.Millisecond)

	if got.Status != "emitted" {
		t.Fatalf("got %+v, want a raised threshold to accept a 600ms-old observation", got)
	}
}

func TestValidateStaleMSAcceptsTheDefaultAndTheBounds(t *testing.T) {
	for _, ms := range []int{DefaultMaxObservationAgeMS, MinStaleMS, MaxStaleMS} {
		if err := ValidateStaleMS(ms); err != nil {
			t.Errorf("ValidateStaleMS(%d): %v", ms, err)
		}
	}
}

func TestValidateStaleMSRefusesTooLow(t *testing.T) {
	// Below the fastest tracking interval (10 Hz = 100ms), there is no
	// round-trip budget left at all - every single step would be refused.
	if err := ValidateStaleMS(MinStaleMS - 1); err == nil {
		t.Error("a threshold tighter than the fastest tracking interval must be refused")
	}
}

func TestValidateStaleMSRefusesTooHigh(t *testing.T) {
	// At or beyond the loop's frame watchdog, a silent camera would already
	// have disarmed the session before an observation could ever get that
	// stale, so the freshness gate would stop meaning anything.
	if err := ValidateStaleMS(MaxStaleMS + 1); err == nil {
		t.Error("a threshold that close to the frame watchdog must be refused")
	}
}

func TestValidateStaleMSRefusesNegative(t *testing.T) {
	// A negative threshold makes the driver refuse every single step while
	// logging the confusing "pozycja starsza niż -100 ms".
	if err := ValidateStaleMS(-100); err == nil {
		t.Error("a negative -stale-ms must be refused at startup, not silently disable every step")
	}
}

func TestDriverDisarmsWhenAnotherWindowTakesFocus(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	em.Window = Window{PID: 99, Path: "/Applications/Safari.app"}

	got := d.Walk("N", fresh)

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if d.Status().Armed {
		t.Error("losing focus must disarm, not merely skip one step")
	}
	if len(em.Events()) != 0 {
		t.Error("no key may be sent to a foreign window")
	}
}

// This used to loop maxTapsPerSecond times against a single flat ceiling.
// That ceiling is now the sum of a fence: walking alone is capped at
// maxWalkTapsPerSecond (3), well under the global maxTapsPerSecond (8), so
// the healing reserve has somewhere to come from. This is a changed
// contract, not a corrected constant - walking on its own no longer gets to
// see the whole ceiling.
func TestDriverEnforcesTapRateWithoutBanking(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	for i := 1; i <= maxWalkTapsPerSecond; i++ {
		if got := d.Walk("N", fresh); got.Status != "emitted" {
			t.Fatalf("tap %d: %+v", i, got)
		}
	}

	over := d.Walk("N", fresh)

	if over.Status != "refused" {
		t.Fatalf("got %+v", over)
	}
	// A quiet stretch must not hand back a burst of unused budget.
	*now = now.Add(1200 * time.Millisecond)
	for i := 1; i <= maxWalkTapsPerSecond; i++ {
		if got := d.Walk("N", fresh); got.Status != "emitted" {
			t.Fatalf("after idle, tap %d: %+v", i, got)
		}
	}
	if got := d.Walk("N", fresh); got.Status != "refused" {
		t.Fatalf("idle time banked extra taps: %+v", got)
	}
}

func TestDriverRunsOneActionAtATime(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	if got := d.UseHotkey("rope", 50*time.Millisecond); got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
	before := len(em.Events())

	// The follower repeats the transition on every reading until the floor
	// changes; the second one must not press the hotkey again.
	got := d.UseHotkey("rope", 50*time.Millisecond)

	if got.Status != "in_progress" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != before {
		t.Fatalf("repeated transition pressed the hotkey again: %v", em.Events())
	}
}

func TestDriverDisarmReleasesHeldKeys(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	em.Window = Window{PID: 99}

	d.Walk("N", fresh)

	if em.Released() != 1 {
		t.Error("disarming must release keys even though emitting is otherwise forbidden")
	}
}

func TestDriverTransitionTapsHotkeyThenClicksPlayerTile(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true
	if err := d.Calibrate(0.42, 0.31); err != nil {
		t.Fatal(err)
	}

	got := d.UseHotkey("rope", 50*time.Millisecond)

	if got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
	want := []string{"tap f7 35ms", "click 0.420 0.310"}
	if ev := em.Events(); len(ev) != 2 || ev[0] != want[0] || ev[1] != want[1] {
		t.Fatalf("got %v, want %v", ev, want)
	}
}

func TestDriverTransitionRefusesWithoutCalibration(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true

	got := d.UseHotkey("rope", 50*time.Millisecond)

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("clicking an unknown screen point is worse than doing nothing")
	}
}

func TestDriverTransitionSkipsClickWhenHotkeyUsesItself(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = false

	d.UseHotkey("rope", 50*time.Millisecond)

	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap f7 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverActionDoneUnblocksTheNextAction(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7", "hole": "f8"}
	d.UseHotkey("rope", 50*time.Millisecond)

	d.ActionDone()

	got := d.UseHotkey("hole", 50*time.Millisecond)
	if got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverRefusesStairsBecauseTheyAreWalkedNotUsed(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}

	got := d.UseHotkey("stairs", 50*time.Millisecond)

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("stairs are climbed by walking; no item is used on them")
	}
}

func TestDriverChecksFocusAgainBeforeTheClick(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true
	d.Calibrate(0.5, 0.5)
	// The window changes during the 120 ms the client needs to arm the
	// crosshair, so the click would land in a foreign window.
	em.OnTap = func() { em.Window = Window{PID: 99} }

	got := d.UseHotkey("rope", 50*time.Millisecond)

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	for _, ev := range em.Events() {
		if strings.HasPrefix(ev, "click") {
			t.Fatalf("clicked after losing focus: %v", em.Events())
		}
	}
}

func TestDriverCalibrateRefusesCoordinatesOutsideScreen(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	if err := d.Calibrate(1.2, 0.5); err == nil {
		t.Error("normalised coordinates must stay within 0-1")
	}
}

func TestDriverSetInputConfigStoresValidHotkeys(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	if err := d.SetInputConfig(map[string]string{"rope": "f7", "hole": "f8"}, true, nil); err != nil {
		t.Fatal(err)
	}

	if d.ActionKeys["rope"] != "f7" || d.ActionKeys["hole"] != "f8" {
		t.Fatalf("got %+v", d.ActionKeys)
	}
	if !d.ClickAfterHotkey {
		t.Error("the click-after-hotkey flag must reach the driver")
	}
	// Submit is the real proof the config actually took effect end to end.
	got := d.UseHotkey("rope", 50*time.Millisecond)
	if got.Status != "refused" || got.Reason != "brak kalibracji kratki postaci" {
		t.Fatalf("got %+v, want a calibration refusal proving the hotkey itself was accepted", got)
	}
}

func TestDriverSetInputConfigRefusesUnknownKey(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetInputConfig(map[string]string{"rope": "control"}, false, nil)

	if err == nil {
		t.Fatal("an unknown key name must be refused")
	}
	// The reason must name which of the four fields was rejected - a typo in
	// one field voids the whole config (SetInputConfig is all-or-nothing), so
	// the user's only way to find the field to fix is this message.
	if !strings.Contains(err.Error(), "rope") {
		t.Errorf("got %q, want it to name the rejected action", err.Error())
	}
	if len(d.ActionKeys) != 0 {
		t.Error("a refused config must not partially apply")
	}
}

func TestDriverSetInputConfigRefusesUnknownActionType(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetInputConfig(map[string]string{"stairs": "f7"}, false, nil)

	if err == nil {
		t.Fatal("stairs are walked, not hotkeyed - an action type outside rope/ladder/hole/shovel must be refused")
	}
}

func TestDriverSetInputConfigAllowsClearingAHotkey(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	if err := d.SetInputConfig(map[string]string{"rope": "f7"}, false, nil); err != nil {
		t.Fatal(err)
	}

	if err := d.SetInputConfig(map[string]string{"rope": ""}, false, nil); err != nil {
		t.Fatal(err)
	}

	if _, ok := d.ActionKeys["rope"]; ok {
		t.Error("an empty key must clear the hotkey, not store an empty string")
	}
}

func TestDriverDefaultsToNumpadDirectionKeys(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Walk("N", fresh)

	if got.Status != "emitted" || got.Key != "numpad8" {
		t.Fatalf("got %+v, want the numpad default for N so a numpad user needs no configuration", got)
	}
	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap numpad8 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverSetInputConfigStoresCustomDirectionKeys(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	// The user's real client is WASD, not numpad: w/s/a/d plus q/e/z/c for
	// the diagonals - none of them anywhere near the built-in numpad layout.
	wasd := map[string]string{
		"N": "w", "S": "s", "W": "a", "E": "d",
		"NW": "q", "NE": "e", "SW": "z", "SE": "c",
	}
	if err := d.SetInputConfig(nil, false, wasd); err != nil {
		t.Fatal(err)
	}

	got := d.Walk("N", fresh)

	if got.Status != "emitted" || got.Key != "w" {
		t.Fatalf("got %+v, want the configured WASD key for N", got)
	}
	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap w 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverSetInputConfigRefusesUnknownDirectionName(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetInputConfig(nil, false, map[string]string{"UP": "w"})

	if err == nil {
		t.Fatal("a direction name outside the eight compass names must be refused")
	}
	if !strings.Contains(err.Error(), "UP") {
		t.Errorf("got %q, want it to name the rejected direction", err.Error())
	}
	// All-or-nothing: the built-in numpad default must survive a refused call.
	if d.DirectionKeys["N"] != "numpad8" {
		t.Error("a refused config must not partially apply, and must not clear the previous mapping")
	}
}

func TestDriverSetInputConfigRefusesUnknownDirectionKey(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetInputConfig(nil, false, map[string]string{"N": "control"})

	if err == nil {
		t.Fatal("an unknown key name for a direction must be refused")
	}
	if !strings.Contains(err.Error(), "N") {
		t.Errorf("got %q, want it to name the rejected direction", err.Error())
	}
}

// Reproduces the WASD-with-no-diagonal case directly: a direction left blank
// must refuse with a reason naming the direction, not emit nothing silently
// and not fall back to decomposing the diagonal into two straight steps.
func TestDriverRefusesDirectionLeftEmptyWithClearReason(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	wasd := map[string]string{"N": "w", "S": "s", "W": "a", "E": "d"} // no diagonals configured
	if err := d.SetInputConfig(nil, false, wasd); err != nil {
		t.Fatal(err)
	}

	got := d.Walk("NE", 50*time.Millisecond)

	if got.Status != "refused" {
		t.Fatalf("got %+v, want a refusal rather than silence for an unconfigured diagonal", got)
	}
	if !strings.Contains(got.Reason, "NE") {
		t.Errorf("got reason %q, want it to name the direction with no configured key", got.Reason)
	}
	if len(em.Events()) != 0 {
		t.Error("an unconfigured direction must not touch the system")
	}
}

func TestDriverWalkRefusesUnknownDirectionName(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Walk("UP", 50*time.Millisecond)

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("an unknown direction name must not touch the system")
	}
}

func TestDriverEmitterFailureDisarmsWithPolishReason(t *testing.T) {
	inner := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	em := &failingEmitter{DryEmitter: inner, err: errors.New("boom")}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}

	got := d.Walk("N", fresh)

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if !strings.HasPrefix(got.Reason, "nie udało się wysłać zdarzenia") {
		t.Fatalf("got %+v", got)
	}
	if d.Status().Armed {
		t.Error("a failed emitter call must leave the driver disarmed")
	}
}

// Walking may not eat the whole ceiling: the healing reserve has to be there
// when it is needed, and a tap already spent cannot be taken back.
func TestWalkingCannotSpendTheHealingReserve(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	for i := 0; i < 3; i++ {
		if got := d.Walk("N", fresh); got.Status != "emitted" {
			t.Fatalf("krok %d: %+v", i, got)
		}
	}
	if got := d.Walk("N", fresh); got.Status != "refused" {
		t.Fatalf("czwarty krok w tej samej sekundzie: %+v", got)
	}
	if got := d.Heal("f1", fresh); got.Status != "emitted" {
		t.Fatalf("leczenie po wyczerpaniu budżetu chodzenia: %+v", got)
	}
	if n := len(em.Events()); n != 4 {
		t.Fatalf("zdarzeń = %d, oczekiwano czterech", n)
	}
}

// Two heals a second, and not a third - the reserve is a limit, not a floor.
func TestHealingHasItsOwnCeiling(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	for i := 0; i < 2; i++ {
		if got := d.Heal("f1", fresh); got.Status != "emitted" {
			t.Fatalf("leczenie %d: %+v", i, got)
		}
	}
	if got := d.Heal("f1", fresh); got.Status != "refused" {
		t.Fatalf("trzecie leczenie w tej samej sekundzie: %+v", got)
	}
}

// Everything that is not healing shares six taps a second. Without this row,
// walking and floor actions could take three each and leave the reserve
// existing only on paper.
func TestNonHealingActionsShareOneBudget(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	if err := d.SetInputConfig(map[string]string{"rope": "f7"}, false, defaultDirectionKeys); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if got := d.Walk("N", fresh); got.Status != "emitted" {
			t.Fatalf("krok %d: %+v", i, got)
		}
	}
	for i := 0; i < 3; i++ {
		got := d.UseHotkey("rope", fresh)
		if got.Status != "emitted" {
			t.Fatalf("akcja %d: %+v", i, got)
		}
		d.ActionDone()
	}
	if got := d.UseHotkey("rope", fresh); got.Status != "refused" {
		t.Fatalf("siódme nieleczące stuknięcie: %+v", got)
	}
	if got := d.Heal("f1", fresh); got.Status != "emitted" {
		t.Fatalf("leczenie przy wyczerpanym budżecie nieleczących: %+v", got)
	}
}

func TestCastAndCancelTargetCountInCombatBudget(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	for i := 0; i < 3; i++ {
		res := d.Cast("f4", 0)
		if res.Status != "emitted" {
			t.Fatalf("czar %d: status = %s, oczekiwano emitted", i, res.Status)
		}
	}
	// Fourth combat-purpose tap in the same second must be refused - Cast and
	// CancelTarget share one budget row.
	if res := d.CancelTarget(0); res.Status != "refused" {
		t.Fatalf("czwarty klawisz walki w tej samej sekundzie: status = %s, oczekiwano refused", res.Status)
	}
}

func TestCastRefusesUnknownKey(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if res := d.Cast("nieznany-klawisz", 0); res.Status != "refused" {
		t.Fatalf("nieznany klawisz czaru: status = %s, oczekiwano refused", res.Status)
	}
}

func TestCancelTargetTapsEscape(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	res := d.CancelTarget(0)
	if res.Status != "emitted" || res.Key != "escape" {
		t.Fatalf("CancelTarget = %+v, oczekiwano emitted/escape", res)
	}
}

func TestCombatBudgetDoesNotBorrowFromHealReserve(t *testing.T) {
	em := &DryEmitter{Window: Window{PID: 1}}
	d := NewDriver(em, DefaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	// Six non-heal taps (the shared non-heal fence) must not block a
	// subsequent heal - the heal reserve stays carved out regardless of how
	// combat taps are spent. The combat row holds three, so two casts plus one
	// CancelTarget fill it, and three steps top the fence up to six; every one
	// of them is checked, because a silently refused tap would leave the fence
	// unfilled and the final heal proving nothing.
	for i := 0; i < 2; i++ {
		if res := d.Cast("f4", 0); res.Status != "emitted" {
			t.Fatalf("czar %d: status = %s, oczekiwano emitted", i, res.Status)
		}
	}
	if res := d.CancelTarget(0); res.Status != "emitted" {
		t.Fatalf("anulowanie celu: status = %s, oczekiwano emitted", res.Status)
	}
	for i := 0; i < 3; i++ {
		if res := d.Walk("N", 0); res.Status != "emitted" {
			t.Fatalf("krok %d: status = %s, oczekiwano emitted", i, res.Status)
		}
	}
	if res := d.Heal("f1", 0); res.Status != "emitted" {
		t.Fatalf("leczenie po sześciu nieleczących stuknięciach: status = %s, oczekiwano emitted", res.Status)
	}
}

func TestValidHotkeyAcceptsEscapeSpaceTab(t *testing.T) {
	for _, k := range []string{"escape", "space", "tab"} {
		if !ValidHotkey(k) {
			t.Errorf("ValidHotkey(%q) = false, oczekiwano true", k)
		}
	}
}

// Re-arming happens after every lost focus. Clearing the tap history there
// would turn the rate limit into a suggestion.
func TestArmingDoesNotClearTheTapHistory(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	for i := 0; i < 2; i++ {
		if got := d.Heal("f1", fresh); got.Status != "emitted" {
			t.Fatalf("leczenie %d: %+v", i, got)
		}
	}
	d.Disarm("test")
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	if got := d.Heal("f1", fresh); got.Status != "refused" {
		t.Fatalf("leczenie po przezbrojeniu: %+v", got)
	}
}

// A floor action stays in flight until the panel confirms the floor changed.
// Healing has nothing to confirm, so it must not be blocked by one - nor may it
// leave an in-flight action of its own behind.
func TestHealingIgnoresAnActionInFlight(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	if err := d.SetInputConfig(map[string]string{"rope": "f7"}, false, defaultDirectionKeys); err != nil {
		t.Fatal(err)
	}
	if got := d.UseHotkey("rope", fresh); got.Status != "emitted" {
		t.Fatalf("akcja: %+v", got)
	}
	if got := d.Heal("f1", fresh); got.Status != "emitted" || got.Key != "f1" {
		t.Fatalf("leczenie w trakcie akcji piętra: %+v", got)
	}
	if got := d.Walk("N", fresh); got.Status != "in_progress" {
		t.Fatalf("leczenie zostawiło po sobie akcję w locie: %+v", got)
	}
	if ev := em.Events(); len(ev) != 2 || ev[1] != "tap f1 35ms" {
		t.Fatalf("zdarzenia = %v", ev)
	}
}

func TestHealingRefusesAnUnknownKey(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	got := d.Heal("klawisz z księżyca", fresh)
	if got.Status != "refused" || !strings.Contains(got.Reason, "klawisz") {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("odrzucony klawisz jednak coś wysłał")
	}
}

// Healing goes through the same gates as everything else: a stale observation
// means the picture the decision was made on is no longer the screen.
func TestHealingRefusesStaleObservation(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	got := d.Heal("f1", time.Duration(DefaultMaxObservationAgeMS+1)*time.Millisecond)
	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("nieświeża obserwacja jednak coś wysłała")
	}
}

// Escape is a valid key name - CancelTarget taps it - but nothing a user
// picks may be bound to it. As a floor action or a direction it would cancel
// the target every time the bot dug or took a step.
func TestDriverSetInputConfigRefusesTheReservedEscapeKey(t *testing.T) {
	for _, tt := range []struct {
		name       string
		keys, dirs map[string]string
	}{
		{"akcja", map[string]string{"rope": "escape"}, nil},
		{"kierunek", nil, map[string]string{"N": "escape"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, _, _ := driverAt(t, time.Unix(0, 0))
			err := d.SetInputConfig(tt.keys, false, tt.dirs)
			if err == nil {
				t.Fatal("zastrzeżony klawisz przeszedł walidację")
			}
			if !strings.Contains(err.Error(), "zastrzeżony") {
				t.Fatalf("powód = %q", err)
			}
			if d.ActionKeys["rope"] == "escape" || d.DirectionKeys["N"] == "escape" {
				t.Errorf("odrzucony klawisz jednak został zapisany: %+v %+v", d.ActionKeys, d.DirectionKeys)
			}
		})
	}
}

// The reserved list must not shrink the set of names the driver itself can
// tap: CancelTarget presses escape, so it has to stay a known key.
func TestDriverEscapeStaysTappableForCancelTarget(t *testing.T) {
	if !hotkeyNames["escape"] {
		t.Fatal("escape musi zostać znanym klawiszem - CancelTarget go stuka")
	}
	if ValidHotkey("escape") != true || !ReservedHotkey("escape") {
		t.Fatal("escape ma być prawidłowy dla sterownika i zastrzeżony dla użytkownika")
	}
}
