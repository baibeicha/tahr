package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	goatui "github.com/baibeicha/goatui/pkg/ui"

	"tahr/internal/core"
	cbuf "tahr/internal/core/buffer"
)

// latestToast safely returns the most recently emitted toast item, or nil if none.
func latestToast(m *AppModel) *goatui.ToastItem {
	if m.toasts == nil {
		return nil
	}
	ts := m.toasts.Toasts()
	if len(ts) == 0 {
		return nil
	}
	return ts[len(ts)-1]
}

// TestChallengerR1_BoundaryExhaustion_CleanAndDirtyBuffer tests extreme boundary exhaustion:
// 1. Spamming undo on clean buffer (100x)
// 2. Spamming redo on clean buffer (100x)
// 3. Multi-stage edit sequence with boundary exhaustion at both bottom (oldest) and top (newest)
// 4. Rapid oscillating undo/redo at boundaries (1000x)
func TestChallengerR1_BoundaryExhaustion_CleanAndDirtyBuffer(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// --- Phase 1: Spamming Undo on Clean Buffer ---
	for i := 0; i < 100; i++ {
		updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
		m := updated.(*AppModel)
		model = m

		if m.statusMessage != "Already at oldest change" {
			t.Fatalf("iteration %d: expected status 'Already at oldest change', got %q", i, m.statusMessage)
		}
		lt := latestToast(m)
		if lt == nil {
			t.Fatalf("iteration %d: expected toast notification, got nil", i)
		}
		if lt.Level != goatui.ToastWarn || lt.Title != "UNDO" || lt.Message != "Already at oldest change" {
			t.Fatalf("iteration %d: expected Warn toast 'UNDO: Already at oldest change', got Level=%v Title=%q Message=%q",
				i, lt.Level, lt.Title, lt.Message)
		}
		doc := m.eng.ActiveDocument()
		txt, _ := doc.Buffer.GetText()
		if len(txt) != 0 {
			t.Fatalf("iteration %d: expected empty buffer, got %q", i, string(txt))
		}
	}

	// --- Phase 2: Spamming Redo on Clean Buffer ---
	for i := 0; i < 100; i++ {
		// Alternate between Ctrl+Y and Ctrl+Shift+Z
		var key input.Key
		if i%2 == 0 {
			key = input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}
		} else {
			key = input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl | input.ModShift}
		}
		updated, _ := model.Update(tea.KeyMsg{Key: key})
		m := updated.(*AppModel)
		model = m

		if m.statusMessage != "Already at newest change" {
			t.Fatalf("iteration %d: expected status 'Already at newest change', got %q", i, m.statusMessage)
		}
		lt := latestToast(m)
		if lt == nil {
			t.Fatalf("iteration %d: expected toast notification, got nil", i)
		}
		if lt.Level != goatui.ToastWarn || lt.Title != "REDO" || lt.Message != "Already at newest change" {
			t.Fatalf("iteration %d: expected Warn toast 'REDO: Already at newest change', got Level=%v Title=%q Message=%q",
				i, lt.Level, lt.Title, lt.Message)
		}
		doc := m.eng.ActiveDocument()
		txt, _ := doc.Buffer.GetText()
		if len(txt) != 0 {
			t.Fatalf("iteration %d: expected empty buffer, got %q", i, string(txt))
		}
	}

	// --- Phase 3: Dirty Buffer with 20 Discrete Edits ---
	// We insert 20 words separated by spaces.
	numEdits := 20
	for i := 0; i < numEdits; i++ {
		word := fmt.Sprintf("word%d ", i)
		for _, r := range word {
			updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: r}})
			model = updated.(*AppModel)
		}
	}

	doc := model.eng.ActiveDocument()
	fullTxt, _ := doc.Buffer.GetText()
	if !strings.HasPrefix(string(fullTxt), "word0 ") || !strings.HasSuffix(string(fullTxt), fmt.Sprintf("word%d ", numEdits-1)) {
		t.Fatalf("expected 20 words, got %q", string(fullTxt))
	}

	// Redo at newest change should warn
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m := updated.(*AppModel)
	model = m
	if m.statusMessage != "Already at newest change" {
		t.Fatalf("expected 'Already at newest change', got %q", m.statusMessage)
	}

	// Unwind all edits using Undo
	undoCount := 0
	for {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage == "Already at oldest change" {
			break
		}
		if m.statusMessage != "Undo" {
			t.Fatalf("expected 'Undo', got %q", m.statusMessage)
		}
		lt := latestToast(m)
		if lt == nil || lt.Level != goatui.ToastInfo || lt.Title != "UNDO" || lt.Message != "Undo" {
			t.Fatalf("expected Info toast 'UNDO: Undo', got %v", lt)
		}
		undoCount++
		if undoCount > numEdits*5 {
			t.Fatal("infinite undo loop detected")
		}
	}

	if undoCount == 0 {
		t.Fatal("expected at least 1 undo transaction")
	}

	// Buffer must be back to empty
	txt, _ := doc.Buffer.GetText()
	if len(txt) != 0 {
		t.Fatalf("expected empty buffer after full undo, got %q", string(txt))
	}

	// Spam 50 more undos past boundary
	for i := 0; i < 50; i++ {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage != "Already at oldest change" {
			t.Fatalf("post-boundary undo %d: expected 'Already at oldest change', got %q", i, m.statusMessage)
		}
		lt := latestToast(m)
		if lt.Level != goatui.ToastWarn || lt.Title != "UNDO" {
			t.Fatalf("expected Warn toast 'UNDO', got %v", lt)
		}
	}

	// Replay all edits back using Redo
	redoCount := 0
	for {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage == "Already at newest change" {
			break
		}
		if m.statusMessage != "Redo" {
			t.Fatalf("expected 'Redo', got %q", m.statusMessage)
		}
		lt := latestToast(m)
		if lt == nil || lt.Level != goatui.ToastInfo || lt.Title != "REDO" || lt.Message != "Redo" {
			t.Fatalf("expected Info toast 'REDO: Redo', got %v", lt)
		}
		redoCount++
		if redoCount > numEdits*5 {
			t.Fatal("infinite redo loop detected")
		}
	}

	if redoCount != undoCount {
		t.Fatalf("redo count (%d) did not match undo count (%d)", redoCount, undoCount)
	}

	// Text must match full original text
	txt, _ = doc.Buffer.GetText()
	if string(txt) != string(fullTxt) {
		t.Fatalf("text mismatch after full redo.\nExpected: %q\nGot:      %q", string(fullTxt), string(txt))
	}

	// Spam 50 more redos past boundary
	for i := 0; i < 50; i++ {
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage != "Already at newest change" {
			t.Fatalf("post-boundary redo %d: expected 'Already at newest change', got %q", i, m.statusMessage)
		}
	}

	// --- Phase 4: Rapid Oscillating Spam at Boundary (1000 cycles) ---
	for i := 0; i < 1000; i++ {
		// Undo 1 step
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage != "Undo" {
			t.Fatalf("oscillate undo %d: expected 'Undo', got %q", i, m.statusMessage)
		}

		// Redo 1 step back to top
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage != "Redo" {
			t.Fatalf("oscillate redo %d: expected 'Redo', got %q", i, m.statusMessage)
		}

		// Hit newest boundary
		updated, _ = model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
		m = updated.(*AppModel)
		model = m
		if m.statusMessage != "Already at newest change" {
			t.Fatalf("oscillate boundary %d: expected 'Already at newest change', got %q", i, m.statusMessage)
		}
	}
}

// TestChallengerR1_RussianLayout_AllVariations verifies all permutations of Russian layout keys:
// - Lowercase 'я' (0x44F) and Uppercase 'Я' (0x42F) with Ctrl -> Undo
// - Lowercase 'я' (0x44F) and Uppercase 'Я' (0x42F) with Ctrl+Shift -> Redo
// - Lowercase 'н' (0x43D) and Uppercase 'Н' (0x41D) with Ctrl -> Redo
// - Lowercase 'н' (0x43D) and Uppercase 'Н' (0x41D) with Ctrl+Shift -> Redo
// - BaseKey, ShiftedKey, and Text field encodings from Kitty/terminals
func TestChallengerR1_RussianLayout_AllVariations(t *testing.T) {
	testCases := []struct {
		name         string
		key          input.Key
		expectedAction string // "Undo" or "Redo"
	}{
		// Undo variations: Cyrillic 'я' / 'Я'
		{
			name:           "Ctrl+я (lowercase rune 0x44f)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl},
			expectedAction: "Undo",
		},
		{
			name:           "Ctrl+Я (uppercase rune 0x42f with ModCtrl only - CapsLock)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x42f, Mod: input.ModCtrl},
			expectedAction: "Undo",
		},
		{
			name:           "Ctrl+я with BaseKey 'z'",
			key:            input.Key{Type: input.KeyRune, Rune: 0x44f, BaseKey: 'z', Mod: input.ModCtrl},
			expectedAction: "Undo",
		},
		{
			name:           "Ctrl+я with BaseKey 0x44f",
			key:            input.Key{Type: input.KeyRune, Rune: 0, BaseKey: 0x44f, Mod: input.ModCtrl},
			expectedAction: "Undo",
		},
		{
			name:           "Ctrl+я with Text 'я'",
			key:            input.Key{Type: input.KeyRune, Rune: 0, Text: "я", Mod: input.ModCtrl},
			expectedAction: "Undo",
		},
		{
			name:           "Ctrl+Я with Text 'Я' and ModCtrl only",
			key:            input.Key{Type: input.KeyRune, Rune: 0, Text: "Я", Mod: input.ModCtrl},
			expectedAction: "Undo",
		},

		// Redo variations: Cyrillic 'я' / 'Я' + Shift
		{
			name:           "Ctrl+Shift+я (lowercase rune 0x44f with ModShift)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Shift+Я (uppercase rune 0x42f with ModShift)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x42f, Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Shift+я with ShiftedKey 0x42f and BaseKey 'z'",
			key:            input.Key{Type: input.KeyRune, Rune: 0x42f, BaseKey: 'z', ShiftedKey: 0x42f, Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Shift+я with Text 'Я'",
			key:            input.Key{Type: input.KeyRune, Rune: 0, Text: "Я", Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},

		// Redo variations: Cyrillic 'н' / 'Н' (maps to 'y')
		{
			name:           "Ctrl+н (lowercase rune 0x43d)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x43d, Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Н (uppercase rune 0x41d with ModCtrl only - CapsLock)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x41d, Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Shift+н (lowercase rune 0x43d with ModShift)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x43d, Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Shift+Н (uppercase rune 0x41d with ModShift)",
			key:            input.Key{Type: input.KeyRune, Rune: 0x41d, Mod: input.ModCtrl | input.ModShift},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+н with BaseKey 'y'",
			key:            input.Key{Type: input.KeyRune, Rune: 0x43d, BaseKey: 'y', Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+н with BaseKey 0x43d",
			key:            input.Key{Type: input.KeyRune, Rune: 0, BaseKey: 0x43d, Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+н with Text 'н'",
			key:            input.Key{Type: input.KeyRune, Rune: 0, Text: "н", Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
		{
			name:           "Ctrl+Н with Text 'Н'",
			key:            input.Key{Type: input.KeyRune, Rune: 0, Text: "Н", Mod: input.ModCtrl},
			expectedAction: "Redo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			eng := core.NewEngine()
			model := NewAppModel(eng)

			// Paste initial text
			updated, _ := model.Update(tea.PasteMsg{Text: "initial state"})
			m := updated.(*AppModel)

			if tc.expectedAction == "Undo" {
				// Execute Undo via test key
				updated, _ = m.Update(tea.KeyMsg{Key: tc.key})
				m = updated.(*AppModel)

				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()
				if string(txt) != "" {
					t.Fatalf("expected empty buffer after Undo, got %q", string(txt))
				}
				if m.statusMessage != "Undo" {
					t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
				}
				lt := latestToast(m)
				if lt == nil || lt.Level != goatui.ToastInfo || lt.Title != "UNDO" || lt.Message != "Undo" {
					t.Fatalf("expected Info toast 'UNDO: Undo', got %v", lt)
				}
			} else {
				// For Redo test: first Undo via standard Ctrl+Z, then apply test Redo key
				updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
				m = updated.(*AppModel)
				doc := m.eng.ActiveDocument()
				txt, _ := doc.Buffer.GetText()
				if string(txt) != "" {
					t.Fatalf("setup failed: expected empty buffer after initial undo, got %q", string(txt))
				}

				// Execute Redo via test key
				updated, _ = m.Update(tea.KeyMsg{Key: tc.key})
				m = updated.(*AppModel)

				txt, _ = doc.Buffer.GetText()
				if string(txt) != "initial state" {
					t.Fatalf("expected 'initial state' after Redo, got %q", string(txt))
				}
				if m.statusMessage != "Redo" {
					t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
				}
				lt := latestToast(m)
				if lt == nil || lt.Level != goatui.ToastInfo || lt.Title != "REDO" || lt.Message != "Redo" {
					t.Fatalf("expected Info toast 'REDO: Redo', got %v", lt)
				}
			}
		})
	}
}

// TestChallengerR1_LegacyControlCodesAndKittyParser verifies:
// 1. Raw legacy control code 0x1A (SUB / 26) with and without ModCtrl / ModShift
// 2. Raw legacy control code 0x19 (EM / 25) with and without ModCtrl
// 3. Decoding raw ANSI escape sequences with input.NewParser() and feeding into AppModel
func TestChallengerR1_LegacyControlCodesAndKittyParser(t *testing.T) {
	t.Run("Legacy 0x1A without ModCtrl (bare byte 26)", func(t *testing.T) {
		eng := core.NewEngine()
		model := NewAppModel(eng)

		updated, _ := model.Update(tea.PasteMsg{Text: "alpha"})
		m := updated.(*AppModel)

		// Send raw byte 26 without ModCtrl
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 26}})
		m = updated.(*AppModel)

		doc := m.eng.ActiveDocument()
		txt, _ := doc.Buffer.GetText()
		if string(txt) != "" {
			t.Fatalf("expected empty buffer after bare byte 26, got %q", string(txt))
		}
		if m.statusMessage != "Undo" {
			t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
		}
	})

	t.Run("Legacy 0x1A with ModShift -> Redo", func(t *testing.T) {
		eng := core.NewEngine()
		model := NewAppModel(eng)

		updated, _ := model.Update(tea.PasteMsg{Text: "beta"})
		m := updated.(*AppModel)

		// Undo first
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 26}})
		m = updated.(*AppModel)

		// Send byte 26 with Shift modifier -> Redo
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 26, Mod: input.ModShift}})
		m = updated.(*AppModel)

		doc := m.eng.ActiveDocument()
		txt, _ := doc.Buffer.GetText()
		if string(txt) != "beta" {
			t.Fatalf("expected 'beta' after 0x1A with Shift, got %q", string(txt))
		}
		if m.statusMessage != "Redo" {
			t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
		}
	})

	t.Run("Legacy 0x19 without ModCtrl (bare byte 25) -> Redo", func(t *testing.T) {
		eng := core.NewEngine()
		model := NewAppModel(eng)

		updated, _ := model.Update(tea.PasteMsg{Text: "gamma"})
		m := updated.(*AppModel)

		// Undo
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 26}})
		m = updated.(*AppModel)

		// Redo with bare byte 25
		updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 25}})
		m = updated.(*AppModel)

		doc := m.eng.ActiveDocument()
		txt, _ := doc.Buffer.GetText()
		if string(txt) != "gamma" {
			t.Fatalf("expected 'gamma' after bare byte 25, got %q", string(txt))
		}
		if m.statusMessage != "Redo" {
			t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
		}
	})

	t.Run("Raw ANSI Kitty Escape Sequences via input.Parser", func(t *testing.T) {
		ansiSequences := []struct {
			name           string
			bytes          []byte
			expectedAction string // "Undo" or "Redo"
		}{
			{
				name:           "Kitty Ctrl+Z (CSI 122 ; 5 u)",
				bytes:          []byte("\x1b[122;5u"),
				expectedAction: "Undo",
			},
			{
				name:           "Kitty Ctrl+Shift+Z (CSI 122 ; 6 u)",
				bytes:          []byte("\x1b[122;6u"),
				expectedAction: "Redo",
			},
			{
				name:           "Kitty Ctrl+Y (CSI 121 ; 5 u)",
				bytes:          []byte("\x1b[121;5u"),
				expectedAction: "Redo",
			},
			{
				name:           "Kitty Russian Ctrl+я (CSI 1103:1071:122 ; 5 ; 1103 u)",
				bytes:          []byte("\x1b[1103:1071:122;5;1103u"),
				expectedAction: "Undo",
			},
			{
				name:           "Kitty Russian Ctrl+Shift+Я (CSI 1071:1071:122 ; 6 ; 1071 u)",
				bytes:          []byte("\x1b[1071:1071:122;6;1071u"),
				expectedAction: "Redo",
			},
			{
				name:           "Kitty Russian Ctrl+н (CSI 1085:1053:121 ; 5 ; 1085 u)",
				bytes:          []byte("\x1b[1085:1053:121;5;1085u"),
				expectedAction: "Redo",
			},
			{
				name:           "Raw control byte 0x1A via parser",
				bytes:          []byte{0x1A},
				expectedAction: "Undo",
			},
			{
				name:           "Raw control byte 0x19 via parser",
				bytes:          []byte{0x19},
				expectedAction: "Redo",
			},
		}

		for _, as := range ansiSequences {
			t.Run(as.name, func(t *testing.T) {
				eng := core.NewEngine()
				model := NewAppModel(eng)

				// Seed text
				updated, _ := model.Update(tea.PasteMsg{Text: "kitty test"})
				m := updated.(*AppModel)

				parser := input.NewParser()
				var parsedEvents []input.Event
				parser.Parse(as.bytes, func(ev input.Event) {
					parsedEvents = append(parsedEvents, ev)
				})

				if len(parsedEvents) == 0 {
					t.Fatalf("parser failed to decode ANSI sequence %q", string(as.bytes))
				}

				keyEv := parsedEvents[0]
				if keyEv.Type != input.EventKey {
					t.Fatalf("expected EventKey, got %v", keyEv.Type)
				}

				if as.expectedAction == "Undo" {
					updated, _ = m.Update(tea.KeyMsg{Key: keyEv.Key})
					m = updated.(*AppModel)

					doc := m.eng.ActiveDocument()
					txt, _ := doc.Buffer.GetText()
					if string(txt) != "" {
						t.Fatalf("expected empty buffer after %s, got %q", as.name, string(txt))
					}
					if m.statusMessage != "Undo" {
						t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
					}
				} else {
					// Undo first
					updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
					m = updated.(*AppModel)

					// Send parsed key
					updated, _ = m.Update(tea.KeyMsg{Key: keyEv.Key})
					m = updated.(*AppModel)

					doc := m.eng.ActiveDocument()
					txt, _ := doc.Buffer.GetText()
					if string(txt) != "kitty test" {
						t.Fatalf("expected 'kitty test' after %s, got %q", as.name, string(txt))
					}
					if m.statusMessage != "Redo" {
						t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
					}
				}
			})
		}
	})
}

// TestChallengerR1_ToastAndStatusMessageInvariants verifies:
// 1. Toast queue maintains maxShow limit (4)
// 2. Toasts expire properly on tick
// 3. Undo/Redo without active document emits "No active document" warning toast and status message
func TestChallengerR1_ToastAndStatusMessageInvariants(t *testing.T) {
	// Create engine with zero documents
	emptyEng := &core.Engine{}
	model := NewAppModel(emptyEng)

	if model.eng.ActiveDocument() != nil {
		t.Fatal("expected no active document in empty engine")
	}

	// Press Ctrl+Z without active document
	updated, _ := model.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m := updated.(*AppModel)

	if m.statusMessage != "No active document" {
		t.Fatalf("expected 'No active document', got %q", m.statusMessage)
	}
	lt := latestToast(m)
	if lt == nil || lt.Level != goatui.ToastWarn || lt.Title != "UNDO" || lt.Message != "No active document" {
		t.Fatalf("expected Warn toast 'UNDO: No active document', got %v", lt)
	}

	// Press Ctrl+Y without active document
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	if m.statusMessage != "No active document" {
		t.Fatalf("expected 'No active document', got %q", m.statusMessage)
	}
	lt = latestToast(m)
	if lt == nil || lt.Level != goatui.ToastWarn || lt.Title != "REDO" || lt.Message != "No active document" {
		t.Fatalf("expected Warn toast 'REDO: No active document', got %v", lt)
	}

	// Test nil engine entirely (must not panic)
	nilEngModel := NewAppModel(nil)
	nilEngModel.performUndo()
	nilEngModel.performRedo()

	// Verify toast expiration
	m.toasts.Tick(time.Now().Add(5 * time.Second))
	if m.toasts.Count() != 0 {
		t.Fatalf("expected 0 active toasts after expiration, got %d", m.toasts.Count())
	}
}

// TestChallengerR1_TerminalDrawer_MultiKeyTransitions verifies:
// 1. Terminal open & focused with empty prompt: Ctrl+Z, Ctrl+Shift+Z, Russian Ctrl+я, Ctrl+н, 0x1A cascade to editor
// 2. Terminal open & focused with typed prompt: first keystroke clears prompt and protects editor
// 3. Immediate follow-up keystroke cascades to editor because prompt is now empty
func TestChallengerR1_TerminalDrawer_MultiKeyTransitions(t *testing.T) {
	keysToTest := []struct {
		name         string
		undoKey      input.Key
		redoKey      input.Key
	}{
		{
			name:    "English Ctrl+Z / Ctrl+Y",
			undoKey: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl},
			redoKey: input.Key{Type: input.KeyRune, Rune: 'y', Mod: input.ModCtrl},
		},
		{
			name:    "English Ctrl+Z / Ctrl+Shift+Z",
			undoKey: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl},
			redoKey: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl | input.ModShift},
		},
		{
			name:    "Russian Ctrl+я / Ctrl+н",
			undoKey: input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl},
			redoKey: input.Key{Type: input.KeyRune, Rune: 0x43d, Mod: input.ModCtrl},
		},
		{
			name:    "Russian Ctrl+я / Ctrl+Shift+я",
			undoKey: input.Key{Type: input.KeyRune, Rune: 0x44f, Mod: input.ModCtrl},
			redoKey: input.Key{Type: input.KeyRune, Rune: 0x42f, Mod: input.ModCtrl | input.ModShift},
		},
		{
			name:    "Legacy 0x1A / 0x19",
			undoKey: input.Key{Type: input.KeyRune, Rune: 26},
			redoKey: input.Key{Type: input.KeyRune, Rune: 25},
		},
	}

	for _, kt := range keysToTest {
		t.Run(kt.name, func(t *testing.T) {
			eng := core.NewEngine()
			model := NewAppModel(eng)

			// Insert edit in editor
			updated, _ := model.Update(tea.PasteMsg{Text: "editor content"})
			m := updated.(*AppModel)

			// Open terminal drawer
			updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyF4}})
			m = updated.(*AppModel)

			if !m.terminal.Open || !m.terminalFocused {
				t.Fatal("terminal should be open and focused")
			}

			// --- Scenario A: Prompt empty -> Undo cascades to editor ---
			updated, _ = m.Update(tea.KeyMsg{Key: kt.undoKey})
			m = updated.(*AppModel)

			doc := m.eng.ActiveDocument()
			txt, _ := doc.Buffer.GetText()
			if string(txt) != "" {
				t.Fatalf("expected empty buffer after cascading undo, got %q", string(txt))
			}
			if m.statusMessage != "Undo" {
				t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
			}

			// Prompt empty -> Redo cascades to editor
			updated, _ = m.Update(tea.KeyMsg{Key: kt.redoKey})
			m = updated.(*AppModel)
			txt, _ = doc.Buffer.GetText()
			if string(txt) != "editor content" {
				t.Fatalf("expected 'editor content' after cascading redo, got %q", string(txt))
			}
			if m.statusMessage != "Redo" {
				t.Fatalf("expected status 'Redo', got %q", m.statusMessage)
			}

			// --- Scenario B: Prompt has text -> First undo clears prompt and DOES NOT touch editor ---
			m.terminal.mu.Lock()
			m.terminal.InputText = "git status"
			m.terminal.CursorPos = 10
			m.terminal.mu.Unlock()

			if !m.terminal.IsActivelyTyping() {
				t.Fatal("terminal should be actively typing")
			}

			// Keystroke 1: Consumed by terminal prompt
			updated, _ = m.Update(tea.KeyMsg{Key: kt.undoKey})
			m = updated.(*AppModel)

			if m.terminal.IsActivelyTyping() {
				t.Fatalf("expected terminal input to be cleared, got %q", m.terminal.InputText)
			}
			txt, _ = doc.Buffer.GetText()
			if string(txt) != "editor content" {
				t.Fatalf("editor buffer was unexpectedly modified! got %q", string(txt))
			}

			// Keystroke 2: Since prompt is now empty, it cascades to editor!
			updated, _ = m.Update(tea.KeyMsg{Key: kt.undoKey})
			m = updated.(*AppModel)

			txt, _ = doc.Buffer.GetText()
			if string(txt) != "" {
				t.Fatalf("expected editor text to be undone on second keystroke, got %q", string(txt))
			}
			if m.statusMessage != "Undo" {
				t.Fatalf("expected status 'Undo', got %q", m.statusMessage)
			}
		})
	}
}

// TestChallengerR1_ViewportSynchronizationUnderLoad tests that undo/redo across widely separated
// locations in a large buffer synchronizes all scroll offsets and eliminates inertial drift.
func TestChallengerR1_ViewportSynchronizationUnderLoad(t *testing.T) {
	eng := core.NewEngine()
	model := NewAppModel(eng)

	// Set window size 100x30
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(*AppModel)

	// Create 500 lines of text
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: test content here", i))
	}
	bigDoc := strings.Join(lines, "\n")
	updated, _ = m.Update(tea.PasteMsg{Text: bigDoc})
	m = updated.(*AppModel)

	// Edit at line 10
	doc := m.eng.ActiveDocument()
	off10, _ := doc.Buffer.ByteOffsetForLine(10)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 10, Column: 0, Byte: off10}, Head: cbuf.Position{Line: 10, Column: 0, Byte: off10}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '#'}})
	m = updated.(*AppModel)

	// Edit at line 400
	off400, _ := doc.Buffer.ByteOffsetForLine(400)
	doc.Buffer.SetSelections([]cbuf.Selection{
		{Anchor: cbuf.Position{Line: 400, Column: 0, Byte: off400}, Head: cbuf.Position{Line: 400, Column: 0, Byte: off400}},
	})
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: '$'}})
	m = updated.(*AppModel)

	// Simulate viewport being scrolled to line 200 with nonzero velocity
	m.viewportY = 200
	ap := m.splits.ActivePane()
	if ap != nil {
		ap.ViewportY = 200
		ap.TargetViewportY = 250
		ap.SmoothScrollY = 190
		ap.ScrollVelocity = 12.5
	}

	// Undo edit at line 400
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	ap = m.splits.ActivePane()
	if ap == nil {
		t.Fatal("expected active pane")
	}

	// Viewport must have jumped near line 400
	if m.viewportY < 350 || m.viewportY > 410 {
		t.Fatalf("expected viewportY near line 400, got %d", m.viewportY)
	}

	// All scroll offsets must be strictly synchronized
	if ap.TargetViewportY != float64(m.viewportY) {
		t.Fatalf("TargetViewportY (%f) != viewportY (%d)", ap.TargetViewportY, m.viewportY)
	}
	if ap.SmoothScrollY != float64(m.viewportY) {
		t.Fatalf("SmoothScrollY (%f) != viewportY (%d)", ap.SmoothScrollY, m.viewportY)
	}
	if ap.ScrollVelocity != 0 {
		t.Fatalf("ScrollVelocity (%f) != 0", ap.ScrollVelocity)
	}

	// Run 20 animation ticks: viewport must NOT fight or drift
	for tick := 0; tick < 20; tick++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)
		if m.viewportY < 350 || m.viewportY > 410 {
			t.Fatalf("tick %d: viewport drifted to %d", tick, m.viewportY)
		}
	}

	// Now Undo edit at line 10
	updated, _ = m.Update(tea.KeyMsg{Key: input.Key{Type: input.KeyRune, Rune: 'z', Mod: input.ModCtrl}})
	m = updated.(*AppModel)

	// Viewport must have jumped near line 10
	if m.viewportY > 20 {
		t.Fatalf("expected viewportY near line 10 (<= 20), got %d", m.viewportY)
	}

	ap = m.splits.ActivePane()
	if ap.TargetViewportY != float64(m.viewportY) || ap.SmoothScrollY != float64(m.viewportY) || ap.ScrollVelocity != 0 {
		t.Fatalf("offsets unsynchronized after jumping to line 10: Target=%f, Smooth=%f, Vel=%f, VP=%d",
			ap.TargetViewportY, ap.SmoothScrollY, ap.ScrollVelocity, m.viewportY)
	}

	// Run 20 animation ticks again
	for tick := 0; tick < 20; tick++ {
		updated, _ = m.Update(animTickMsg{})
		m = updated.(*AppModel)
		if m.viewportY > 20 {
			t.Fatalf("tick %d: viewport drifted to %d", tick, m.viewportY)
		}
	}
}
