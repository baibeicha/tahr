//go:build windows

package main

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/baibeicha/goatui/pkg/driver/input"
)

func TestRawConsoleReader_CtrlZAndUtf8(t *testing.T) {
	// Simulate decoding UTF-16 units into rawConsoleReader.utf8Buf
	utf16Units := []uint16{
		'h', 'e', 'l', 'l', 'o',
		0x001A, // Ctrl+Z
		0x044F, // Russian small letter 'я'
		'w', 'o', 'r', 'l', 'd',
	}

	reader := &rawConsoleReader{
		utf16Buf: make([]uint16, 256),
		utf8Buf:  make([]byte, 0, 1024),
	}

	for _, u := range utf16Units {
		reader.utf8Buf = utf8.AppendRune(reader.utf8Buf, rune(u))
	}

	buf := make([]byte, 64)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	data := buf[:n]
	if !bytes.Contains(data, []byte{0x1A}) {
		t.Fatalf("expected data to contain raw byte 0x1A (Ctrl+Z), got: %v", data)
	}

	// Feed through input.Parser and ensure EventKey with Rune 'z' and ModCtrl is produced
	parser := input.NewParser()
	var ctrlZReceived bool
	parser.Parse([]byte{0x1A}, func(ev input.Event) {
		if ev.Type == input.EventKey && ev.Key.Rune == 'z' && ev.Key.Mod == input.ModCtrl {
			ctrlZReceived = true
		}
	})

	if !ctrlZReceived {
		t.Fatalf("input.Parser failed to parse byte 0x1A into Ctrl+Z key event")
	}
}
