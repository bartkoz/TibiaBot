package main

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
	d := NewDriver(em, defaultMaxObservationAgeMS)
	d.now = func() time.Time { return now }
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	return d, em, &now
}

func walk(seq uint64, session string) Intent {
	return Intent{Session: session, Seq: seq, Action: "walk", Direction: "N", AgeMS: 100}
}

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
	d := NewDriver(em, defaultMaxObservationAgeMS)

	got := d.Submit(walk(1, "nieistniejąca"))

	if got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("a disarmed driver must not touch the system")
	}
}

func TestDriverEmitsOneTapPerAcceptedIntent(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Submit(walk(1, d.Status().Session))

	if got.Status != "emitted" || got.Key != "numpad8" {
		t.Fatalf("got %+v", got)
	}
	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap numpad8 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverRejectsForeignSessionToken(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))

	got := d.Submit(walk(1, "podrobiony"))

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("a wrong token must not reach the system")
	}
}

func TestDriverRepeatedSeqReturnsFirstResultWithoutPressingAgain(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session
	first := d.Submit(walk(7, session))

	again := d.Submit(walk(7, session))

	if again != first {
		t.Fatalf("got %+v, want %+v", again, first)
	}
	if len(em.Events()) != 1 {
		t.Fatalf("a retried request pressed the key twice: %v", em.Events())
	}
}

func TestDriverRefusesStaleObservation(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	in := walk(1, d.Status().Session)
	in.AgeMS = defaultMaxObservationAgeMS + 1

	got := d.Submit(in)

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
	in := walk(1, d.Status().Session)
	in.AgeMS = 600 // stale under the 400ms default, fresh under 1000ms

	got := d.Submit(in)

	if got.Status != "emitted" {
		t.Fatalf("got %+v, want a raised threshold to accept a 600ms-old observation", got)
	}
}

func TestDriverDisarmsWhenAnotherWindowTakesFocus(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	em.Window = Window{PID: 99, Path: "/Applications/Safari.app"}

	got := d.Submit(walk(1, d.Status().Session))

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

func TestDriverEnforcesTapRateWithoutBanking(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session
	for i := uint64(1); i <= maxTapsPerSecond; i++ {
		if got := d.Submit(walk(i, session)); got.Status != "emitted" {
			t.Fatalf("tap %d: %+v", i, got)
		}
	}

	over := d.Submit(walk(99, session))

	if over.Status != "refused" {
		t.Fatalf("got %+v", over)
	}
	// A quiet stretch must not hand back a burst of unused budget. The clock
	// moves in heartbeat-sized steps, or the driver would disarm instead.
	for range 6 {
		*now = now.Add(200 * time.Millisecond)
		d.Beat(session)
	}
	for i := uint64(100); i < 100+maxTapsPerSecond; i++ {
		if got := d.Submit(walk(i, session)); got.Status != "emitted" {
			t.Fatalf("after idle, tap %d: %+v", i, got)
		}
	}
	if got := d.Submit(walk(200, session)); got.Status != "refused" {
		t.Fatalf("idle time banked extra taps: %+v", got)
	}
}

func TestDriverExpiresWhenHeartbeatStops(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	*now = now.Add(time.Duration(heartbeatTimeoutMS+1) * time.Millisecond)

	if got := d.Submit(walk(1, session)); got.Status != "disarmed" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverHeartbeatKeepsTheSessionAlive(t *testing.T) {
	d, _, now := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	for range 5 {
		*now = now.Add(200 * time.Millisecond)
		d.Beat(session)
	}

	if got := d.Submit(walk(1, session)); got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverRunsOneActionAtATime(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	session := d.Status().Session
	rope := Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 3, AgeMS: 50}
	if got := d.Submit(rope); got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
	before := len(em.Events())

	// The follower repeats 'transition' on every reading until the floor
	// changes; the second one must not press the hotkey again.
	rope.Seq = 2
	got := d.Submit(rope)

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

	d.Submit(walk(1, d.Status().Session))

	if em.Released() != 1 {
		t.Error("disarming must release keys even though emitting is otherwise forbidden")
	}
}

func TestDriverRefusesSeqGoingBackwards(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	d.Submit(walk(5, session))
	got := d.Submit(walk(3, session))

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 1 {
		t.Fatalf("a Seq going backwards must not press a key: %v", em.Events())
	}
}

func TestDriverRefusesMissingSeq(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	session := d.Status().Session

	got := d.Submit(walk(0, session))

	if got.Status != "refused" {
		t.Fatalf("got %+v", got)
	}
	if len(em.Events()) != 0 {
		t.Error("Seq 0 must not press a key")
	}
}

func TestDriverTransitionTapsHotkeyThenClicksPlayerTile(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}
	d.ClickAfterHotkey = true
	if err := d.Calibrate(0.42, 0.31); err != nil {
		t.Fatal(err)
	}
	session := d.Status().Session

	got := d.Submit(Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 2, AgeMS: 50})

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

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

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

	d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

	if ev := em.Events(); len(ev) != 1 || ev[0] != "tap f7 35ms" {
		t.Fatalf("got %v", ev)
	}
}

func TestDriverActionDoneUnblocksTheNextAction(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7", "hole": "f8"}
	session := d.Status().Session
	d.Submit(Intent{Session: session, Seq: 1, Action: "transition", Type: "rope", Waypoint: 1, AgeMS: 50})

	d.ActionDone()

	got := d.Submit(Intent{Session: session, Seq: 2, Action: "transition", Type: "hole", Waypoint: 2, AgeMS: 50})
	if got.Status != "emitted" {
		t.Fatalf("got %+v", got)
	}
}

func TestDriverRefusesStairsBecauseTheyAreWalkedNotUsed(t *testing.T) {
	d, em, _ := driverAt(t, time.Unix(0, 0))
	d.ActionKeys = map[string]string{"rope": "f7"}

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "stairs", AgeMS: 50})

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

	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})

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

func TestDriverSetActionConfigStoresValidHotkeys(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	if err := d.SetActionConfig(map[string]string{"rope": "f7", "hole": "f8"}, true); err != nil {
		t.Fatal(err)
	}

	if d.ActionKeys["rope"] != "f7" || d.ActionKeys["hole"] != "f8" {
		t.Fatalf("got %+v", d.ActionKeys)
	}
	if !d.ClickAfterHotkey {
		t.Error("the click-after-hotkey flag must reach the driver")
	}
	// Submit is the real proof the config actually took effect end to end.
	got := d.Submit(Intent{Session: d.Status().Session, Seq: 1, Action: "transition", Type: "rope", AgeMS: 50})
	if got.Status != "refused" || got.Reason != "brak kalibracji kratki postaci" {
		t.Fatalf("got %+v, want a calibration refusal proving the hotkey itself was accepted", got)
	}
}

func TestDriverSetActionConfigRefusesUnknownKey(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetActionConfig(map[string]string{"rope": "control"}, false)

	if err == nil {
		t.Fatal("an unknown key name must be refused")
	}
	// The reason must name which of the four fields was rejected - a typo in
	// one field voids all four (SetActionConfig is all-or-nothing), so the
	// user's only way to find the field to fix is this message.
	if !strings.Contains(err.Error(), "rope") {
		t.Errorf("got %q, want it to name the rejected action", err.Error())
	}
	if len(d.ActionKeys) != 0 {
		t.Error("a refused config must not partially apply")
	}
}

func TestDriverSetActionConfigRefusesUnknownActionType(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))

	err := d.SetActionConfig(map[string]string{"stairs": "f7"}, false)

	if err == nil {
		t.Fatal("stairs are walked, not hotkeyed - an action type outside rope/ladder/hole/shovel must be refused")
	}
}

func TestDriverSetActionConfigAllowsClearingAHotkey(t *testing.T) {
	d, _, _ := driverAt(t, time.Unix(0, 0))
	if err := d.SetActionConfig(map[string]string{"rope": "f7"}, false); err != nil {
		t.Fatal(err)
	}

	if err := d.SetActionConfig(map[string]string{"rope": ""}, false); err != nil {
		t.Fatal(err)
	}

	if _, ok := d.ActionKeys["rope"]; ok {
		t.Error("an empty key must clear the hotkey, not store an empty string")
	}
}

func TestDriverEmitterFailureDisarmsWithPolishReason(t *testing.T) {
	inner := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	em := &failingEmitter{DryEmitter: inner, err: errors.New("boom")}
	d := NewDriver(em, defaultMaxObservationAgeMS)
	if _, err := d.Arm(); err != nil {
		t.Fatal(err)
	}
	session := d.Status().Session

	got := d.Submit(walk(1, session))

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
