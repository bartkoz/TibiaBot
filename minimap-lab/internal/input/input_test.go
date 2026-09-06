package input

import "testing"

// hotkeyNames is the portable table /api/input/config validates a submitted
// key name against; it must build (and be correct) on every platform, so its
// coverage of the ANSI letters and top-row digits is tested here rather than
// in a platform-tagged file.
func TestHotkeyNamesCoversLettersAndDigits(t *testing.T) {
	for c := byte('a'); c <= 'z'; c++ {
		if name := string(rune(c)); !hotkeyNames[name] {
			t.Errorf("hotkeyNames is missing letter %q", name)
		}
	}
	for c := byte('0'); c <= '9'; c++ {
		if name := string(rune(c)); !hotkeyNames[name] {
			t.Errorf("hotkeyNames is missing digit %q", name)
		}
	}
	if hotkeyNames["control"] {
		t.Error("hotkeyNames must not accept an arbitrary unknown name")
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
