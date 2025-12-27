package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

type KeyboardBackend interface {
	Press(key string) error
	Release(key string) error
	Close() error
}

// UInput backend - works on both X11 and Wayland
type UInputBackend struct {
	fd       int
	keyState map[string]bool
}

// X11 backend using xdotool
type X11Backend struct {
	keyState map[string]bool
}

// Key code mappings for uinput (Linux input event codes)
var linuxKeyCodes = map[string]uint16{
	"W": 17, "A": 30, "S": 31, "D": 32,
	"w": 17, "a": 30, "s": 31, "d": 32,
	"UP": 103, "DOWN": 108, "LEFT": 105, "RIGHT": 106,
	"up": 103, "down": 108, "left": 105, "right": 106,
	"SPACE": 57, "space": 57,
	"ENTER": 28, "enter": 28,
	"Q": 16, "E": 18, "R": 19, "F": 33,
	"q": 16, "e": 18, "r": 19, "f": 33,
	"1": 2, "2": 3, "3": 4, "4": 5,
	"5": 6, "6": 7, "7": 8, "8": 9, "9": 10, "0": 11,
	"I": 23, "J": 36, "K": 37, "L": 38,
	"i": 23, "j": 36, "k": 37, "l": 38,
	"U": 22, "O": 24, "P": 25,
	"u": 22, "o": 24, "p": 25,
}

// uinput ioctl constants
const (
	UINPUT_MAX_NAME_SIZE = 80
	UI_DEV_CREATE        = 0x5501
	UI_DEV_DESTROY       = 0x5502
	UI_SET_EVBIT         = 0x40045564
	UI_SET_KEYBIT        = 0x40045565
	EV_KEY               = 0x01
	EV_SYN               = 0x00
	SYN_REPORT           = 0x00
)

type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

type uinputUserDev struct {
	Name       [UINPUT_MAX_NAME_SIZE]byte
	ID         inputID
	EffectsMax uint32
	Absmax     [64]int32
	Absmin     [64]int32
	Absfuzz    [64]int32
	Absflat    [64]int32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

func NewUInputBackend() (*UInputBackend, error) {
	fd, err := syscall.Open("/dev/uinput", syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/uinput: %v (try running as root or add user to 'input' group)", err)
	}

	// Set up event types
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), UI_SET_EVBIT, EV_KEY); errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to set EV_KEY: %v", errno)
	}

	// Register all keys we might use
	for _, code := range linuxKeyCodes {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), UI_SET_KEYBIT, uintptr(code)); errno != 0 {
			syscall.Close(fd)
			return nil, fmt.Errorf("failed to set key %d: %v", code, errno)
		}
	}

	// Create device
	uidev := uinputUserDev{
		ID: inputID{
			Bustype: 0x03, // BUS_USB
			Vendor:  0x1234,
			Product: 0x5678,
			Version: 1,
		},
	}
	copy(uidev.Name[:], "DivaController")

	if _, err := syscall.Write(fd, (*[unsafe.Sizeof(uidev)]byte)(unsafe.Pointer(&uidev))[:]); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to write device info: %v", err)
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), UI_DEV_CREATE, 0); errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("failed to create device: %v", errno)
	}

	return &UInputBackend{
		fd:       fd,
		keyState: make(map[string]bool),
	}, nil
}

func (u *UInputBackend) sendEvent(typ uint16, code uint16, value int32) error {
	var tv syscall.Timeval
	syscall.Gettimeofday(&tv)

	ev := inputEvent{
		Time:  tv,
		Type:  typ,
		Code:  code,
		Value: value,
	}

	_, err := syscall.Write(u.fd, (*[unsafe.Sizeof(ev)]byte)(unsafe.Pointer(&ev))[:])
	return err
}

func (u *UInputBackend) sync() error {
	return u.sendEvent(EV_SYN, SYN_REPORT, 0)
}

func (u *UInputBackend) Press(key string) error {
	if u.keyState[key] {
		return nil
	}
	code, ok := linuxKeyCodes[key]
	if !ok {
		return fmt.Errorf("unknown key: %s", key)
	}
	if err := u.sendEvent(EV_KEY, code, 1); err != nil {
		return err
	}
	if err := u.sync(); err != nil {
		return err
	}
	u.keyState[key] = true
	return nil
}

func (u *UInputBackend) Release(key string) error {
	if !u.keyState[key] {
		return nil
	}
	code, ok := linuxKeyCodes[key]
	if !ok {
		return fmt.Errorf("unknown key: %s", key)
	}
	if err := u.sendEvent(EV_KEY, code, 0); err != nil {
		return err
	}
	if err := u.sync(); err != nil {
		return err
	}
	u.keyState[key] = false
	return nil
}

func (u *UInputBackend) Close() error {
	// Release all pressed keys
	for key := range u.keyState {
		u.Release(key)
	}
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(u.fd), UI_DEV_DESTROY, 0)
	return syscall.Close(u.fd)
}

// X11 backend using xdotool
func NewX11Backend() (*X11Backend, error) {
	// Check if xdotool is available
	if _, err := exec.LookPath("xdotool"); err != nil {
		return nil, fmt.Errorf("xdotool not found: %v", err)
	}
	return &X11Backend{keyState: make(map[string]bool)}, nil
}

func (x *X11Backend) Press(key string) error {
	if x.keyState[key] {
		return nil
	}
	cmd := exec.Command("xdotool", "keydown", strings.ToLower(key))
	if err := cmd.Run(); err != nil {
		return err
	}
	x.keyState[key] = true
	return nil
}

func (x *X11Backend) Release(key string) error {
	if !x.keyState[key] {
		return nil
	}
	cmd := exec.Command("xdotool", "keyup", strings.ToLower(key))
	if err := cmd.Run(); err != nil {
		return err
	}
	x.keyState[key] = false
	return nil
}

func (x *X11Backend) Close() error {
	for key := range x.keyState {
		x.Release(key)
	}
	return nil
}

// Auto-detect backend
func NewKeyboardBackend() (KeyboardBackend, error) {
	// Try uinput first (works on both X11 and Wayland)
	if backend, err := NewUInputBackend(); err == nil {
		fmt.Println("[Keyboard] Using uinput backend (works on X11 and Wayland)")
		return backend, nil
	}

	// Check if we're on X11
	if os.Getenv("DISPLAY") != "" {
		if backend, err := NewX11Backend(); err == nil {
			fmt.Println("[Keyboard] Using X11/xdotool backend")
			return backend, nil
		}
	}

	return nil, fmt.Errorf("no keyboard backend available. For uinput: run as root or add user to 'input' group. For X11: install xdotool")
}
