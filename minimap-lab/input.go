package main

import (
	"errors"
	"fmt"
	"sync"
)

// ErrUnsupported is returned by platforms with no emitter implementation.
var ErrUnsupported = errors.New("sterowanie nie jest dostępne na tej platformie")

// Window identifies the application that currently owns keyboard focus.
// Path is the executable path on Windows and the bundle identifier on macOS;
// the title is diagnostic only, because any browser tab can be called "Tibia".
type Window struct {
	PID   int    `json:"pid"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

// Emitter is the whole surface the driver needs from the operating system.
// Keeping it this small is what lets the driver be tested without a graphical
// session: every safety rule lives above this line.
type Emitter interface {
	TapKey(key string, holdMS int) error
	Click(nx, ny float64) error // normalised 0-1 within the shared screen
	Focused() (Window, error)
	ReleaseAll() error
	Preflight() error
}

// hotkeyNames lists every key name the platform emitters recognise for a
// floor-action hotkey or a direction. input_darwin.go and input_windows.go
// each carry the real per-platform codes for the same set of names; this
// portable table is the one inputapi.go's /api/input/config handler checks a
// submitted key against, since it must build (and validate) on every
// platform regardless of which emitter is actually running.
var hotkeyNames = map[string]bool{
	"numpad1": true, "numpad2": true, "numpad3": true, "numpad4": true,
	"numpad6": true, "numpad7": true, "numpad8": true, "numpad9": true,
	"up": true, "down": true, "left": true, "right": true,
	"f1": true, "f2": true, "f3": true, "f4": true, "f5": true, "f6": true,
	"f7": true, "f8": true, "f9": true, "f10": true, "f11": true, "f12": true,
}

// init adds the ANSI letter keys a-z and the top-row digits 0-9, so a client
// whose movement is bound to letters (WASD and the like) has a key name to
// configure at all - darwinKeys and windowsKeys each carry the matching
// per-platform codes for the same names.
func init() {
	for c := byte('a'); c <= 'z'; c++ {
		hotkeyNames[string(rune(c))] = true
	}
	for c := byte('0'); c <= '9'; c++ {
		hotkeyNames[string(rune(c))] = true
	}
}

// DryEmitter performs no system calls. It backs the -input dry mode and every
// driver test, so the whole flow can be exercised without the game.
type DryEmitter struct {
	Window Window
	OnTap  func()

	mu       sync.Mutex
	events   []string
	released int
}

func (e *DryEmitter) TapKey(key string, holdMS int) error {
	e.mu.Lock()
	e.events = append(e.events, fmt.Sprintf("tap %s %dms", key, holdMS))
	hook := e.OnTap
	e.mu.Unlock()
	// OnTap lets a test change the focused window mid-sequence, the way a real
	// alt-tab would between the hotkey and the click.
	if hook != nil {
		hook()
	}
	return nil
}

func (e *DryEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem: %.3f %.3f", nx, ny)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, fmt.Sprintf("click %.3f %.3f", nx, ny))
	return nil
}

func (e *DryEmitter) Focused() (Window, error) { return e.Window, nil }

func (e *DryEmitter) ReleaseAll() error {
	e.mu.Lock()
	e.released++
	e.mu.Unlock()
	return nil
}

func (e *DryEmitter) Preflight() error { return nil }

func (e *DryEmitter) Events() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

// Released counts emergency releases, so tests can prove that disarming
// actually reached the system rather than merely flipping a flag.
func (e *DryEmitter) Released() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.released
}
