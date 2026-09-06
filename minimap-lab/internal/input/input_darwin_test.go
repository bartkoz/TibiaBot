//go:build darwin

package input

import "testing"

// darwinKeys carries the real macOS virtual key codes for every name
// hotkeyNames (the portable table /api/input/config validates against)
// claims to know. A name missing here would pass validation and then fail
// at TapKey with "nieznany klawisz", so completeness matters as much as the
// individual values.
func TestDarwinKeysCoversEveryHotkeyName(t *testing.T) {
	for name := range hotkeyNames {
		if _, ok := darwinKeys[name]; !ok {
			t.Errorf("darwinKeys is missing %q, known to hotkeyNames", name)
		}
	}
}

// These four pin the WASD cluster's physical-position codes against the
// live-client report that drove this feature: a is 0, s is 1, d is 2, w is
// 13. A copy-paste mistake here (e.g. alphabetical order instead of physical
// position) would send the wrong key while still looking plausible.
func TestDarwinKeysWASDClusterPhysicalPositions(t *testing.T) {
	want := map[string]uint16{"a": 0, "s": 1, "d": 2, "w": 13}
	for key, code := range want {
		if got := darwinKeys[key]; got != code {
			t.Errorf("darwinKeys[%q] = %d, want %d", key, got, code)
		}
	}
}

// The user's real client binds diagonals to q/e/z/c around the WASD
// cluster - the panel's WASD preset relies on all four resolving to a real
// code.
func TestDarwinKeysWASDDiagonals(t *testing.T) {
	for _, key := range []string{"q", "e", "z", "c"} {
		if _, ok := darwinKeys[key]; !ok {
			t.Errorf("darwinKeys is missing diagonal key %q", key)
		}
	}
}
