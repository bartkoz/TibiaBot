package core

import "testing"

func TestPressKeyRecorded(t *testing.T) {
	var pressed []string
	PressKeyFunc = func(key string) { pressed = append(pressed, key) }
	defer func() { PressKeyFunc = defaultPressKey }()

	PressKey("F1")
	if len(pressed) != 1 || pressed[0] != "F1" {
		t.Errorf("pressed = %v, want [F1]", pressed)
	}
}

func TestPressKeysSimultaneousRecorded(t *testing.T) {
	var pressed []string
	PressKeysSimultaneousFunc = func(keys []string) { pressed = append(pressed, keys...) }
	defer func() { PressKeysSimultaneousFunc = defaultPressKeysSimultaneous }()

	PressKeysSimultaneous([]string{"up", "right"})
	if len(pressed) != 2 {
		t.Errorf("pressed = %v, want [up right]", pressed)
	}
}
