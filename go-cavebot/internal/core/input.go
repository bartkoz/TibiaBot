package core

import (
	"time"

	"github.com/go-vgo/robotgo"
)

// PressKeyFunc and PressKeysSimultaneousFunc are replaceable for testing.
var (
	PressKeyFunc              = defaultPressKey
	PressKeysSimultaneousFunc = defaultPressKeysSimultaneous
)

func PressKey(key string) {
	PressKeyFunc(key)
}

func PressKeysSimultaneous(keys []string) {
	PressKeysSimultaneousFunc(keys)
}

func defaultPressKey(key string) {
	robotgo.KeyTap(key)
}

func defaultPressKeysSimultaneous(keys []string) {
	for _, k := range keys {
		robotgo.KeyToggle(k, "down")
	}
	time.Sleep(20 * time.Millisecond)
	for i := len(keys) - 1; i >= 0; i-- {
		robotgo.KeyToggle(keys[i], "up")
	}
}
