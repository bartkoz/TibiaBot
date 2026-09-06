//go:build darwin

package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ANSI physical key codes. These are positions on the keyboard, not ASCII and
// not Windows virtual keys.
var darwinKeys = map[string]uint16{
	"numpad1": 83, "numpad2": 84, "numpad3": 85, "numpad4": 86,
	"numpad6": 88, "numpad7": 89, "numpad8": 91, "numpad9": 92,
	"up": 126, "down": 125, "left": 123, "right": 124,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	// ANSI letters, keyed by physical position (kVK_ANSI_*), not by
	// alphabetical order or ASCII value - e.g. "a" is 0, "s" is 1, "d" is 2,
	// "w" is 13, matching a US/ANSI keyboard's WASD cluster.
	"a": 0, "b": 11, "c": 8, "d": 2, "e": 14, "f": 3, "g": 5, "h": 4,
	"i": 34, "j": 38, "k": 40, "l": 37, "m": 46, "n": 45, "o": 31, "p": 35,
	"q": 12, "r": 15, "s": 1, "t": 17, "u": 32, "v": 9, "w": 13, "x": 7,
	"y": 16, "z": 6,
	// Top-row digits (kVK_ANSI_0..9), also physical positions rather than
	// ASCII order.
	"0": 29, "1": 18, "2": 19, "3": 20, "4": 21,
	"5": 23, "6": 22, "7": 26, "8": 28, "9": 25,
}

const (
	kCGHIDEventTap        = 0
	kCGEventLeftMouseDown = 1
	kCGEventLeftMouseUp   = 2
	kCGEventMouseMoved    = 5
	kCGMouseButtonLeft    = 0
)

// cgPoint stands in for CGPoint: two float64 fields, passed by value.
type cgPoint struct{ X, Y float64 }

// cgSize stands in for CGSize: two float64 fields, passed by value.
type cgSize struct{ Width, Height float64 }

// cgRect stands in for CGRect: a 32-byte struct (two nested two-float64
// structs), returned by value. CGDisplayBounds is the only function here
// that returns a struct rather than a scalar/pointer; purego's amd64 and
// arm64 struct-return paths were verified empirically before relying on this
// (see the fix-round report), including on arm64 where a 32-byte struct only
// stays in registers if purego recognises it as a homogeneous
// floating-point aggregate.
type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

type darwinEmitter struct {
	mu sync.Mutex
	// held is the set of physical key codes still down. Unlike Windows there
	// is no extended-key bit to remember per key: CGEventCreateKeyboardEvent's
	// down/up flag plus the same CGKeyCode is everything needed to reconstruct
	// the up event later, so a set is enough.
	held map[uint16]bool

	createKeyboard func(source uintptr, key uint16, down bool) uintptr
	createMouse    func(source uintptr, kind uint32, at cgPoint, button uint32) uintptr
	post           func(tap uint32, event uintptr)
	release        func(ref uintptr)
	preflight      func() bool
	displayBounds  func(display uint32) cgRect
	mainDisplay    func() uint32

	// frontmostApp returns a reference to NSWorkspace's current
	// frontmostApplication, or 0 if there is none. Focused() calls this exactly
	// once and derives both the PID and the name from that single reference -
	// calling it twice (once per field) would let the frontmost app change
	// between the two calls, so PID and Path could end up describing two
	// different applications.
	frontmostApp func() uintptr
	pidOfApp     func(app uintptr) int
	nameOfApp    func(app uintptr) string
}

// registerFunc wraps purego.RegisterLibFunc, which panics when name cannot be
// resolved in the library, and turns that panic into a plain error instead.
// CGPreflightPostEventAccess, for instance, only exists from macOS 10.15
// onward; without this, a missing symbol on an older system would crash the
// whole panel process instead of surfacing the clean error that
// selectEmitter (in inputapi.go) is built to report.
func registerFunc(fptr any, handle uintptr, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("nie udało się powiązać funkcji %s: %v", name, r)
		}
	}()
	purego.RegisterLibFunc(fptr, handle, name)
	return nil
}

func newSystemEmitter() (Emitter, error) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreGraphics: %w", err)
	}
	e := &darwinEmitter{held: map[uint16]bool{}}
	// CGKeyCode is uint16, C bool is one byte, CGPoint/CGRect are structs
	// passed/returned by value. purego does not verify any of this; a wrong
	// declaration fails at run time, not at compile time.
	cgFuncs := []struct {
		fptr any
		name string
	}{
		{&e.createKeyboard, "CGEventCreateKeyboardEvent"},
		{&e.createMouse, "CGEventCreateMouseEvent"},
		{&e.post, "CGEventPost"},
		{&e.preflight, "CGPreflightPostEventAccess"},
		{&e.displayBounds, "CGDisplayBounds"},
		// Display id 0 is not the main display; it has to be asked for by name.
		{&e.mainDisplay, "CGMainDisplayID"},
	}
	for _, f := range cgFuncs {
		if err := registerFunc(f.fptr, cg, f.name); err != nil {
			return nil, err
		}
	}

	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreFoundation: %w", err)
	}
	if err := registerFunc(&e.release, cf, "CFRelease"); err != nil {
		return nil, err
	}

	if err := e.bindWorkspace(); err != nil {
		return nil, err
	}
	return e, nil
}

// Preflight refuses to arm without Accessibility. Without this check
// CGEventPost silently does nothing and the bot merely looks frozen.
func (e *darwinEmitter) Preflight() error {
	if !e.preflight() {
		return fmt.Errorf("brak zgody Accessibility. Dodaj program w Ustawieniach → Prywatność i ochrona → Dostępność, a następnie uruchom panel ponownie — macOS zwykle wymaga restartu procesu, aby nowo nadane uprawnienie zaczęło działać. Zgoda na nagrywanie ekranu, którą ma przeglądarka, jej nie zastępuje")
	}
	return nil
}

func (e *darwinEmitter) TapKey(key string, ms int) error {
	code, ok := darwinKeys[key]
	if !ok {
		return fmt.Errorf("nieznany klawisz: %s", key)
	}
	down := e.createKeyboard(0, code, true)
	if down == 0 {
		return fmt.Errorf("nie udało się utworzyć zdarzenia klawiatury")
	}
	e.post(kCGHIDEventTap, down)
	e.release(down)
	// Only register the key as held once the key-down actually landed, so a
	// failed key-down does not leave a phantom entry for ReleaseAll to chase.
	e.mu.Lock()
	e.held[code] = true
	e.mu.Unlock()

	time.Sleep(time.Duration(ms) * time.Millisecond)

	up := e.createKeyboard(0, code, false)
	if up == 0 {
		return fmt.Errorf("nie udało się utworzyć zdarzenia klawiatury (zwolnienie)")
	}
	e.post(kCGHIDEventTap, up)
	e.release(up)
	// Only forget the key once the key-up actually succeeded; otherwise the
	// game may still see it held and ReleaseAll must be able to retry it.
	e.mu.Lock()
	delete(e.held, code)
	e.mu.Unlock()
	return nil
}

func (e *darwinEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem")
	}
	// CGEventCreateMouseEvent's coordinates are global points, not pixels:
	// CGDisplayPixelsWide/High report backing pixels, which on a Retina
	// display is twice the point count CGEventPost actually expects, and
	// using them would silently place every click at the wrong position.
	// CGDisplayBounds is unambiguously in points and also carries the
	// display's origin, which a plain size would ignore.
	display := e.mainDisplay()
	bounds := e.displayBounds(display)
	at := cgPoint{
		X: bounds.Origin.X + nx*bounds.Size.Width,
		Y: bounds.Origin.Y + ny*bounds.Size.Height,
	}
	for _, kind := range []uint32{kCGEventMouseMoved, kCGEventLeftMouseDown, kCGEventLeftMouseUp} {
		ev := e.createMouse(0, kind, at, kCGMouseButtonLeft)
		if ev == 0 {
			return fmt.Errorf("nie udało się utworzyć zdarzenia myszy")
		}
		e.post(kCGHIDEventTap, ev)
		e.release(ev)
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func (e *darwinEmitter) ReleaseAll() error {
	e.mu.Lock()
	pending := make([]uint16, 0, len(e.held))
	for code := range e.held {
		pending = append(pending, code)
	}
	e.mu.Unlock()

	// Only drop a key from held once its release actually succeeded, so a
	// failed release stays tracked and can be retried instead of being
	// silently forgotten.
	var firstErr error
	released := make([]uint16, 0, len(pending))
	for _, code := range pending {
		up := e.createKeyboard(0, code, false)
		if up == 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("nie udało się utworzyć zdarzenia klawiatury dla kodu: %d", code)
			}
			continue
		}
		e.post(kCGHIDEventTap, up)
		e.release(up)
		released = append(released, code)
	}

	e.mu.Lock()
	for _, code := range released {
		delete(e.held, code)
	}
	e.mu.Unlock()
	return firstErr
}

func (e *darwinEmitter) Focused() (Window, error) {
	// A single lookup: deriving PID and name from two separate calls to
	// frontmostApplication could describe two different apps if focus changed
	// in between, and this is the display the user relies on to confirm what
	// actually got armed.
	app := e.frontmostApp()
	if app == 0 {
		return Window{}, fmt.Errorf("nie udało się odczytać aktywnej aplikacji")
	}
	pid := e.pidOfApp(app)
	if pid == 0 {
		return Window{}, fmt.Errorf("nie udało się odczytać aktywnej aplikacji")
	}
	name := e.nameOfApp(app)
	return Window{PID: pid, Path: name, Title: name}, nil
}

// cString allocates a NUL-terminated byte buffer holding s and returns it
// together with a pointer to its first byte. Converting that pointer to a
// uintptr severs it from Go's garbage collector, so the buffer is not
// guaranteed to outlive the conversion on its own: the caller must keep buf
// alive (via runtime.KeepAlive) until the C call using ptr has returned.
func cString(s string) (buf []byte, ptr uintptr) {
	buf = append([]byte(s), 0)
	return buf, uintptr(unsafe.Pointer(&buf[0]))
}

// goString copies a NUL-terminated C string found at c into a new Go string.
// c must stay valid for the duration of this call; nothing about it is
// retained afterwards.
func goString(c uintptr) string {
	// Take the address of c and dereference it as *unsafe.Pointer rather than
	// converting the uintptr directly, so go vet does not flag this as a
	// possible misuse of unsafe.Pointer: c is a raw C address handed back by
	// objc_msgSend, never a Go pointer that round-tripped through uintptr, so
	// the check's premise does not apply here. Mirrors purego's own internal
	// GoString helper, which is unexported and so cannot be called directly.
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for *(*byte)(unsafe.Add(ptr, uintptr(length))) != 0 {
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}

// bindWorkspace wires up NSWorkspace.frontmostApplication through the plain
// objc runtime functions (no CGO, no purego/objc package). It is chosen over
// CGWindowListCopyWindowInfo because that call is not an unambiguous stand-in
// for keyboard focus, and reading window titles through it would additionally
// require Screen Recording consent.
func (e *darwinEmitter) bindWorkspace() error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
		purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return fmt.Errorf("nie udało się otworzyć AppKit: %w", err)
	}
	getClass, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_getClass")
	if err != nil {
		return err
	}
	registerSel, err := purego.Dlsym(purego.RTLD_DEFAULT, "sel_registerName")
	if err != nil {
		return err
	}
	msgSend, err := purego.Dlsym(purego.RTLD_DEFAULT, "objc_msgSend")
	if err != nil {
		return err
	}
	class := func(name string) uintptr {
		buf, ptr := cString(name)
		r, _, _ := purego.SyscallN(getClass, ptr)
		runtime.KeepAlive(buf)
		return r
	}
	sel := func(name string) uintptr {
		buf, ptr := cString(name)
		r, _, _ := purego.SyscallN(registerSel, ptr)
		runtime.KeepAlive(buf)
		return r
	}
	send := func(obj, s uintptr) uintptr {
		r, _, _ := purego.SyscallN(msgSend, obj, s)
		return r
	}
	workspace := class("NSWorkspace")
	shared, frontmost := sel("sharedWorkspace"), sel("frontmostApplication")
	pidSel, idSel, utf8Sel := sel("processIdentifier"), sel("bundleIdentifier"), sel("UTF8String")

	e.frontmostApp = func() uintptr { return send(send(workspace, shared), frontmost) }
	e.pidOfApp = func(a uintptr) int { return int(int32(send(a, pidSel))) }
	e.nameOfApp = func(a uintptr) string {
		id := send(a, idSel)
		if id == 0 {
			return ""
		}
		ptr := send(id, utf8Sel)
		if ptr == 0 {
			return ""
		}
		return goString(ptr)
	}
	return nil
}
