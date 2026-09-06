package main

import "testing"

func TestKeyForDirectionCoversEightDirections(t *testing.T) {
	want := map[string]string{
		"NW": "numpad7", "N": "numpad8", "NE": "numpad9",
		"W": "numpad4", "E": "numpad6",
		"SW": "numpad1", "S": "numpad2", "SE": "numpad3",
	}
	for dir, key := range want {
		got, ok := keyForDirection(dir)
		if !ok || got != key {
			t.Errorf("%s: got %q %v, want %q", dir, got, ok, key)
		}
	}
	if _, ok := keyForDirection("UP"); ok {
		t.Error("unknown direction must be refused, not guessed")
	}
	if _, ok := keyForDirection(""); ok {
		t.Error("empty direction must be refused")
	}
}

func TestDryEmitterRecordsEventsAndReleasesKeys(t *testing.T) {
	e := &DryEmitter{Window: Window{PID: 42, Path: "/Applications/Tibia.app"}}
	if err := e.TapKey("numpad8", 35); err != nil {
		t.Fatal(err)
	}
	if err := e.Click(0.5, 0.5); err != nil {
		t.Fatal(err)
	}
	got := e.Events()
	if len(got) != 2 || got[0] != "tap numpad8 35ms" || got[1] != "click 0.500 0.500" {
		t.Fatalf("got %v", got)
	}
	if err := e.ReleaseAll(); err != nil || e.Released() != 1 {
		t.Fatalf("released %d, err %v", e.Released(), err)
	}
}

func TestDryEmitterRefusesClickOutsideScreen(t *testing.T) {
	e := &DryEmitter{}
	if err := e.Click(1.5, 0.5); err == nil {
		t.Error("normalised coordinates outside 0-1 must be refused")
	}
}
