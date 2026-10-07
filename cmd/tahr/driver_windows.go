//go:build windows

package main

import (
	"bufio"
	"io"
	"os"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/baibeicha/goatui/pkg/driver"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"golang.org/x/sys/windows"
)

// rawConsoleReader reads directly from a Windows console handle using ReadConsoleW,
// decoding UTF-16 code units to UTF-8 without dropping byte 0x1A (Ctrl-Z).
//
// Background: Go standard library's internal/poll.fd_windows.go (*FD).readConsole()
// explicitly treats 0x1A as EOF (returning 0 bytes and nil error) because of legacy
// MS-DOS CP/M file conventions. This causes os.Stdin.Read() in Windows console (cmd.exe)
// to swallow Ctrl+Z and terminates terminal read loops prematurely.
type rawConsoleReader struct {
	handle         windows.Handle
	fallback       io.Reader
	utf16Buf       []uint16
	utf8Buf        []byte
	savedSurrogate rune
}

func newRawConsoleReader(h windows.Handle, fallback io.Reader) io.Reader {
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return fallback
	}
	return &rawConsoleReader{
		handle:   h,
		fallback: fallback,
		utf16Buf: make([]uint16, 256),
		utf8Buf:  make([]byte, 0, 1024),
	}
}

func (r *rawConsoleReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	for len(r.utf8Buf) == 0 {
		var nw uint32
		err := windows.ReadConsole(r.handle, &r.utf16Buf[0], uint32(len(r.utf16Buf)), &nw, nil)
		if err != nil {
			if r.fallback != nil {
				return r.fallback.Read(p)
			}
			return 0, err
		}
		if nw == 0 {
			return 0, nil
		}

		u16 := r.utf16Buf[:nw]
		for i := 0; i < len(u16); i++ {
			rn := rune(u16[i])
			if r.savedSurrogate != 0 {
				rn = utf16.DecodeRune(r.savedSurrogate, rn)
				r.savedSurrogate = 0
			} else if utf16.IsSurrogate(rn) {
				if i+1 < len(u16) {
					rn = utf16.DecodeRune(rn, rune(u16[i+1]))
					i++
				} else {
					r.savedSurrogate = rn
					continue
				}
			}
			// Important: Preserve 0x1A (Ctrl-Z) and all other control characters.
			r.utf8Buf = utf8.AppendRune(r.utf8Buf, rn)
		}
	}

	n := copy(p, r.utf8Buf)
	r.utf8Buf = r.utf8Buf[n:]
	return n, nil
}

type windowsConsoleDriver struct {
	hStdin      windows.Handle
	hStdout     windows.Handle
	conInFile   *os.File
	conOutFile  *os.File
	inReader    io.Reader
	origInMode  uint32
	origOutMode uint32
	parser      *input.Parser
	events      chan input.Event
	closeOnce   sync.Once
	stopChan    chan struct{}
	outWriter   *bufio.Writer
}

func newTerminalDriver() (driver.Driver, error) {
	hIn := windows.Handle(os.Stdin.Fd())
	hOut := windows.Handle(os.Stdout.Fd())
	var conInFile *os.File
	var conOutFile *os.File
	var inReader io.Reader
	outWriter := bufio.NewWriterSize(os.Stdout, 32768)

	var mode uint32
	if err := windows.GetConsoleMode(hIn, &mode); err != nil {
		if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			conInFile = f
			hIn = windows.Handle(f.Fd())
			inReader = newRawConsoleReader(hIn, f)
		} else {
			inReader = os.Stdin
		}
	} else {
		inReader = newRawConsoleReader(hIn, os.Stdin)
	}

	if err := windows.GetConsoleMode(hOut, &mode); err != nil {
		if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			conOutFile = f
			hOut = windows.Handle(f.Fd())
			outWriter = bufio.NewWriterSize(f, 32768)
		}
	}

	return &windowsConsoleDriver{
		hStdin:     hIn,
		hStdout:    hOut,
		conInFile:  conInFile,
		conOutFile: conOutFile,
		inReader:   inReader,
		parser:     input.NewParser(),
		events:     make(chan input.Event, 256),
		stopChan:   make(chan struct{}),
		outWriter:  outWriter,
	}, nil
}

func (d *windowsConsoleDriver) Init() error {
	if err := windows.GetConsoleMode(d.hStdin, &d.origInMode); err != nil {
		d.origInMode = 0
	}
	if err := windows.GetConsoleMode(d.hStdout, &d.origOutMode); err != nil {
		d.origOutMode = 0
	}

	outMode := d.origOutMode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	_ = windows.SetConsoleMode(d.hStdout, outMode)

	inMode := uint32(windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_WINDOW_INPUT | windows.ENABLE_MOUSE_INPUT)
	_ = windows.SetConsoleMode(d.hStdin, inMode)

	driver.RegisterTeardown(func() {
		_ = d.Close()
	})

	initSeq := "\x1b[?1049h" +
		"\x1b[2J\x1b[H" +
		"\x1b[?25l" +
		"\x1b[?7l" +
		"\x1b[?1000h\x1b[?1003h\x1b[?1006h" +
		"\x1b[?1004h" +
		"\x1b[?2004h" +
		input.KittyPushFlags(input.KittyModeDisambiguateEscapeCodes) + input.KittyQuery()

	_, _ = d.outWriter.WriteString(initSeq)
	_ = d.outWriter.Flush()

	go d.readLoop()
	return nil
}

func (d *windowsConsoleDriver) Close() error {
	d.closeOnce.Do(func() {
		close(d.stopChan)

		exitSeq := "\x1b[?1006l\x1b[?1003l\x1b[?1002l\x1b[?1000l" +
			"\x1b[?1004l\x1b[?2026l" +
			"\x1b[?2004l" +
			"\x1b[?7h" +
			input.KittyDisable() +
			"\x1b[?1049l" +
			"\x1b[?25h\x1b[0m"

		_, _ = d.outWriter.WriteString(exitSeq)
		_ = d.outWriter.Flush()

		if d.origInMode != 0 {
			_ = windows.SetConsoleMode(d.hStdin, d.origInMode)
		}
		if d.origOutMode != 0 {
			_ = windows.SetConsoleMode(d.hStdout, d.origOutMode)
		}
		if d.conInFile != nil {
			_ = d.conInFile.Close()
		}
		if d.conOutFile != nil {
			_ = d.conOutFile.Close()
		}
	})
	return nil
}

func (d *windowsConsoleDriver) Size() (int, int, error) {
	var csbi windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(d.hStdout, &csbi); err == nil {
		w := int(csbi.Window.Right - csbi.Window.Left + 1)
		h := int(csbi.Window.Bottom - csbi.Window.Top + 1)
		if w > 0 && h > 0 {
			return w, h, nil
		}
	}
	return 80, 24, nil
}

func (d *windowsConsoleDriver) Events() <-chan input.Event {
	return d.events
}

func (d *windowsConsoleDriver) Writer() io.Writer {
	return d.outWriter
}

func (d *windowsConsoleDriver) Flush() error {
	return d.outWriter.Flush()
}

func (d *windowsConsoleDriver) readLoop() {
	buf := make([]byte, 1024)
	for {
		select {
		case <-d.stopChan:
			return
		default:
			n, err := d.inReader.Read(buf)
			if err != nil {
				if d.conInFile == nil {
					if conIn, ferr := os.OpenFile("CONIN$", os.O_RDWR, 0); ferr == nil {
						d.conInFile = conIn
						d.hStdin = windows.Handle(conIn.Fd())
						inMode := uint32(windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_WINDOW_INPUT | windows.ENABLE_MOUSE_INPUT)
						_ = windows.SetConsoleMode(d.hStdin, inMode)
						d.inReader = newRawConsoleReader(d.hStdin, conIn)
						continue
					}
				}
				return
			}
			if n == 0 {
				continue
			}
			d.parser.Parse(buf[:n], func(ev input.Event) {
				select {
				case d.events <- ev:
				case <-d.stopChan:
				}
			})
		}
	}
}
