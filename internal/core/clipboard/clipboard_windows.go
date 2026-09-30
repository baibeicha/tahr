//go:build windows

package clipboard

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13 // CF_UNICODETEXT
	gmemMoveable  = 0x0002
)

// toPointer safely reinterprets a C/Windows heap memory address as an unsafe.Pointer.
func toPointer(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func readOS() (string, error) {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return "", err
	}
	defer procCloseClipboard.Call()

	hData, _, err := procGetClipboardData.Call(uintptr(cfUnicodeText))
	if hData == 0 {
		return "", errors.New("no unicode text in clipboard")
	}

	ptr, _, err := procGlobalLock.Call(hData)
	if ptr == 0 {
		return "", err
	}
	defer procGlobalUnlock.Call(hData)

	p := (*uint16)(toPointer(ptr))
	length := 0
	for *(*uint16)(toPointer(ptr + uintptr(length*2))) != 0 {
		length++
	}
	u16 := unsafe.Slice(p, length)
	return syscall.UTF16ToString(u16), nil
}

func writeOS(text string) error {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return err
	}
	defer procCloseClipboard.Call()

	r, _, err = procEmptyClipboard.Call()
	if r == 0 {
		return err
	}

	u16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}

	bytesLen := uintptr(len(u16) * 2)
	hMem, _, err := procGlobalAlloc.Call(uintptr(gmemMoveable), bytesLen)
	if hMem == 0 {
		return err
	}

	ptr, _, err := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return err
	}

	p := (*uint16)(toPointer(ptr))
	dest := unsafe.Slice(p, len(u16))
	copy(dest, u16)
	procGlobalUnlock.Call(hMem)

	r, _, err = procSetClipboardData.Call(uintptr(cfUnicodeText), hMem)
	if r == 0 {
		return err
	}

	return nil
}
