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

// Diagonals are single keys. Composing them from two arrow presses depends on
// event ordering inside the client and is unreliable.
var directionKeys = map[string]string{
	"NW": "numpad7", "N": "numpad8", "NE": "numpad9",
	"W": "numpad4", "E": "numpad6",
	"SW": "numpad1", "S": "numpad2", "SE": "numpad3",
}

func keyForDirection(dir string) (string, bool) {
	key, ok := directionKeys[dir]
	return key, ok
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
