//go:build windows

package input

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procSendInput            = user32.NewProc("SendInput")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procMapVirtualKeyW       = user32.NewProc("MapVirtualKeyW")
	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procQueryFullProcessName = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	keyeventfScanCode    = 0x0008

	mouseeventfMove        = 0x0001
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfVirtualDesk = 0x4000
	mouseeventfAbsolute    = 0x8000

	processQueryLimitedInformation = 0x1000
)

// keyboardInput and mouseInput both stand in for INPUT, whose union member is
// the larger MOUSEINPUT. Both structs must therefore be the same size: 40
// bytes on amd64. The trailing padding on keyboardInput is what makes that so.
type keyboardInput struct {
	kind    uint32
	_       uint32 // union alignment on 64-bit
	wVk     uint16
	wScan   uint16
	flags   uint32
	time    uint32
	extra   uintptr
	padding [8]byte
}

type mouseInput struct {
	kind  uint32
	_     uint32
	dx    int32
	dy    int32
	data  uint32
	flags uint32
	time  uint32
	extra uintptr
}

// Virtual key codes for the keys the panel can send. Scan codes are derived
// from these at send time, because game clients frequently ignore events that
// carry only a virtual key.
var windowsKeys = map[string]uint16{
	"numpad1": 0x61, "numpad2": 0x62, "numpad3": 0x63, "numpad4": 0x64,
	"numpad6": 0x66, "numpad7": 0x67, "numpad8": 0x68, "numpad9": 0x69,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
	"f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
}

// init adds the ANSI letter keys a-z and the top-row digits 0-9. Unlike the
// macOS physical-position codes in input_darwin.go, Windows virtual-key codes
// for these conveniently equal their uppercase ASCII value ("a" is 0x41, "0"
// is 0x30), so they are computed here instead of hand-listed.
func init() {
	for c := byte('a'); c <= 'z'; c++ {
		windowsKeys[string(rune(c))] = uint16('A' + (c - 'a'))
	}
	for c := byte('0'); c <= '9'; c++ {
		windowsKeys[string(rune(c))] = uint16(c)
	}
}

// Arrows need the extended-key flag or the client reads them as their numpad
// twins.
var windowsExtended = map[string]bool{"up": true, "down": true, "left": true, "right": true}

type windowsEmitter struct {
	mu sync.Mutex
	// held maps a virtual key still down to the exact key-down flags used for
	// it (without keyeventfKeyUp), so ReleaseAll can reproduce the extended-key
	// bit that distinguishes an arrow from its numpad twin.
	held map[uint16]uint32
}

func NewSystemEmitter() (Emitter, error) {
	return &windowsEmitter{held: map[uint16]uint32{}}, nil
}

func (e *windowsEmitter) Preflight() error { return nil }

func (e *windowsEmitter) TapKey(key string, ms int) error {
	vk, ok := windowsKeys[key]
	if !ok {
		return fmt.Errorf("nieznany klawisz: %s", key)
	}
	scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC
	if scan == 0 {
		return fmt.Errorf("brak kodu skanowania dla klawisza: %s", key)
	}
	flags := uint32(keyeventfScanCode)
	if windowsExtended[key] {
		flags |= keyeventfExtendedKey
	}
	if err := e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan), flags: flags}); err != nil {
		return err
	}
	// Only register the key as held once the key-down actually landed, so a
	// failed key-down does not leave a phantom entry for ReleaseAll to chase.
	e.mu.Lock()
	e.held[vk] = flags
	e.mu.Unlock()
	time.Sleep(time.Duration(ms) * time.Millisecond)
	err := e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan), flags: flags | keyeventfKeyUp})
	if err == nil {
		// Only forget the key once the key-up actually succeeded; otherwise the
		// game may still see it held and ReleaseAll must be able to retry it.
		e.mu.Lock()
		delete(e.held, vk)
		e.mu.Unlock()
	}
	return err
}

func (e *windowsEmitter) Click(nx, ny float64) error {
	if nx < 0 || nx > 1 || ny < 0 || ny > 1 {
		return fmt.Errorf("współrzędne kliknięcia poza ekranem")
	}
	// Absolute mouse coordinates are always 0-65535 across the virtual desktop,
	// independent of resolution and DPI.
	x, y := int32(nx*65535), int32(ny*65535)
	base := uint32(mouseeventfAbsolute | mouseeventfVirtualDesk)
	for _, flag := range []uint32{mouseeventfMove, mouseeventfLeftDown, mouseeventfLeftUp} {
		if err := e.sendMouse(mouseInput{kind: inputMouse, dx: x, dy: y, flags: base | flag}); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func (e *windowsEmitter) ReleaseAll() error {
	e.mu.Lock()
	pending := make(map[uint16]uint32, len(e.held))
	for vk, flags := range e.held {
		pending[vk] = flags
	}
	e.mu.Unlock()

	// Only drop a key from held once its release actually succeeded, so a
	// failed release stays tracked and can be retried instead of being
	// silently forgotten.
	var firstErr error
	released := make([]uint16, 0, len(pending))
	for vk, flags := range pending {
		scan, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0)
		if scan == 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("brak kodu skanowania dla klawisza wirtualnego: 0x%02X", vk)
			}
			continue
		}
		if err := e.sendKey(keyboardInput{kind: inputKeyboard, wScan: uint16(scan),
			flags: flags | keyeventfKeyUp}); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		released = append(released, vk)
	}

	e.mu.Lock()
	for _, vk := range released {
		delete(e.held, vk)
	}
	e.mu.Unlock()
	return firstErr
}

func (e *windowsEmitter) Focused() (Window, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return Window{}, fmt.Errorf("brak aktywnego okna")
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return Window{PID: int(pid), Path: processPath(pid), Title: syscall.UTF16ToString(buf)}, nil
}

// processPath is the identity that matters. A browser tab can be titled
// "Tibia", so the title alone must never gate emission. Given C1, this is the
// user's only confirmation of what actually got armed, so a lookup failure
// must be visible - never silently indistinguishable from a real process
// that simply has an empty path.
func processPath(pid uint32) string {
	h, _, err := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return fmt.Sprintf("(nie udało się odczytać ścieżki: %v)", err)
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	ok, _, err := procQueryFullProcessName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return fmt.Sprintf("(nie udało się odczytać ścieżki: %v)", err)
	}
	return syscall.UTF16ToString(buf[:size])
}

func (e *windowsEmitter) sendKey(in keyboardInput) error {
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		// UIPI blocks input aimed at a process running at a higher integrity
		// level, and reports it exactly this way.
		return fmt.Errorf("SendInput odrzucone (uruchom panel z tymi samymi uprawnieniami co grę): %v", err)
	}
	return nil
}

func (e *windowsEmitter) sendMouse(in mouseInput) error {
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		return fmt.Errorf("SendInput odrzucone: %v", err)
	}
	return nil
}

// SendInput reads an array of INPUT records of one fixed size; the structs
// standing in for it must match that size exactly, or the call silently
// misbehaves. This layout is amd64-specific: the real Win32 INPUT union is 40
// bytes there. On 386 it is only 28 bytes, so these two structs would need a
// different layout on that architecture — assert the exact size so a 386
// build fails loudly at compile time instead of silently sending misaligned
// data.
var _ [40]byte = [unsafe.Sizeof(keyboardInput{})]byte{}
var _ [40]byte = [unsafe.Sizeof(mouseInput{})]byte{}
