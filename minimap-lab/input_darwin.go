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
	displayWide    func(display uint32) uint64
	displayHigh    func(display uint32) uint64
	mainDisplay    func() uint32

	frontmostPID  func() int
	frontmostName func() string
}

func newSystemEmitter() (Emitter, error) {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreGraphics: %w", err)
	}
	e := &darwinEmitter{held: map[uint16]bool{}}
	// CGKeyCode is uint16, C bool is one byte, CGPoint is two float64 by value.
	// purego does not verify any of this; a wrong declaration fails at run
	// time, not at compile time.
	purego.RegisterLibFunc(&e.createKeyboard, cg, "CGEventCreateKeyboardEvent")
	purego.RegisterLibFunc(&e.createMouse, cg, "CGEventCreateMouseEvent")
	purego.RegisterLibFunc(&e.post, cg, "CGEventPost")
	purego.RegisterLibFunc(&e.preflight, cg, "CGPreflightPostEventAccess")
	purego.RegisterLibFunc(&e.displayWide, cg, "CGDisplayPixelsWide")
	purego.RegisterLibFunc(&e.displayHigh, cg, "CGDisplayPixelsHigh")
	// Display id 0 is not the main display; it has to be asked for by name.
	purego.RegisterLibFunc(&e.mainDisplay, cg, "CGMainDisplayID")

	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("nie udało się otworzyć CoreFoundation: %w", err)
	}
	purego.RegisterLibFunc(&e.release, cf, "CFRelease")

	if err := e.bindWorkspace(); err != nil {
		return nil, err
	}
	return e, nil
}

// Preflight refuses to arm without Accessibility. Without this check
// CGEventPost silently does nothing and the bot merely looks frozen.
func (e *darwinEmitter) Preflight() error {
	if !e.preflight() {
		return fmt.Errorf("brak zgody Accessibility. Dodaj program w Ustawieniach → Prywatność i ochrona → Dostępność. Zgoda na nagrywanie ekranu, którą ma przeglądarka, jej nie zastępuje")
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
	// CGEvent works in points while the shared image is in physical pixels;
	// normalised coordinates make the Retina factor cancel out.
	display := e.mainDisplay()
	at := cgPoint{X: nx * float64(e.displayWide(display)), Y: ny * float64(e.displayHigh(display))}
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
	pid := e.frontmostPID()
	if pid == 0 {
		return Window{}, fmt.Errorf("nie udało się odczytać aktywnej aplikacji")
	}
	name := e.frontmostName()
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

	app := func() uintptr { return send(send(workspace, shared), frontmost) }
	e.frontmostPID = func() int {
		a := app()
		if a == 0 {
			return 0
		}
		return int(int32(send(a, pidSel)))
	}
	e.frontmostName = func() string {
		a := app()
		if a == 0 {
			return ""
		}
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
