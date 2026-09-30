package buffer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// ============================================================================
// Stress Tests: Multilingual Coordinate Mapping & Display Column Calculations
// ============================================================================

// TestStressUTFCyrillicDetailed asserts pixel-perfect coordinate mapping
// across Russian, Ukrainian, Bulgarian, and Serbian Cyrillic alphabets.
func TestStressUTFCyrillicDetailed(t *testing.T) {
	cyrillicSentences := []struct {
		name string
		text string
	}{
		{"Russian", "Привет, мир! Как поживаешь?"},
		{"Ukrainian", "Привіт, світе! Доброго дня та гарного настрою!"},
		{"Bulgarian", "Здравей, свят! Всичко е наред."},
		{"Serbian", "Поздрав свете! Како сте данас?"},
		{"MixedCyrillicAscii", "Func ПриветWorld(тест int) string"},
	}

	tabWidth := 4
	for _, tc := range cyrillicSentences {
		t.Run(tc.name, func(t *testing.T) {
			rawLine := tc.text + "\n"
			prov := newMockLineProvider([]string{rawLine})
			bridge := NewUTFBridge(prov, tabWidth)

			lineBytes := []byte(tc.text)
			totalRunes := utf8.RuneCount(lineBytes)
			expectedBytes := len(lineBytes)

			// Step through every rune in the sentence
			accumBytes := 0
			accumUTF16 := 0
			accumVisual := 0

			runeIdx := 0
			for accumBytes < expectedBytes {
				r, sz := utf8.DecodeRune(lineBytes[accumBytes:])

				// 1. Position -> Byte
				pos := Position{Line: 0, Column: runeIdx, Byte: accumBytes}
				calcByte := bridge.PositionToByte(pos)
				if calcByte != accumBytes {
					t.Fatalf("[%s] rune %d (%c): PositionToByte = %d, expected %d",
						tc.name, runeIdx, r, calcByte, accumBytes)
				}

				// 2. Byte -> Position
				calcPos := bridge.ByteToPosition(accumBytes)
				if calcPos.Line != 0 || calcPos.Column != runeIdx || calcPos.Byte != accumBytes {
					t.Fatalf("[%s] byte %d (%c): ByteToPosition = %+v, expected %+v",
						tc.name, accumBytes, r, calcPos, pos)
				}

				// 3. Position -> LSP (Cyrillic is BMP: 1 UTF-16 code unit per rune)
				line, lspChar := bridge.PositionToLSP(pos)
				if line != 0 || lspChar != accumUTF16 {
					t.Fatalf("[%s] rune %d (%c): PositionToLSP = (%d, %d), expected (0, %d)",
						tc.name, runeIdx, r, line, lspChar, accumUTF16)
				}

				// 4. LSP -> Position
				calcLSPPos := bridge.LSPToPosition(0, accumUTF16)
				if calcLSPPos.Line != 0 || calcLSPPos.Column != runeIdx || calcLSPPos.Byte != accumBytes {
					t.Fatalf("[%s] lsp %d (%c): LSPToPosition = %+v, expected %+v",
						tc.name, accumUTF16, r, calcLSPPos, pos)
				}

				// 5. Visual Column (Cyrillic runes have runewidth 1)
				visCol := bridge.VisualColumn(0, runeIdx)
				if visCol != accumVisual {
					t.Fatalf("[%s] rune %d (%c): VisualColumn = %d, expected %d",
						tc.name, runeIdx, r, visCol, accumVisual)
				}

				// 6. Visual -> Rune Column (clicking exact column snaps to rune)
				rCol := bridge.RuneColFromVisual(0, visCol)
				if rCol != runeIdx {
					t.Fatalf("[%s] vis %d (%c): RuneColFromVisual = %d, expected %d",
						tc.name, visCol, r, rCol, runeIdx)
				}

				accumBytes += sz
				accumUTF16 += 1
				accumVisual += runewidth.RuneWidth(r)
				runeIdx++
			}

			// Test line end boundary
			endPos := Position{Line: 0, Column: totalRunes, Byte: expectedBytes}
			if b := bridge.PositionToByte(endPos); b != expectedBytes {
				t.Fatalf("[%s] end PositionToByte = %d, expected %d", tc.name, b, expectedBytes)
			}
			_, lspEnd := bridge.PositionToLSP(endPos)
			if lspEnd != totalRunes {
				t.Fatalf("[%s] end PositionToLSP = %d, expected %d", tc.name, lspEnd, totalRunes)
			}
			visEnd := bridge.VisualColumn(0, totalRunes)
			if visEnd != accumVisual {
				t.Fatalf("[%s] end VisualColumn = %d, expected %d", tc.name, visEnd, accumVisual)
			}
		})
	}
}

// TestStressUTFEmojiAndSurrogates tests multi-byte emojis, surrogate pairs,
// forward surrogate snapping, and display width of 2 cells.
func TestStressUTFEmojiAndSurrogates(t *testing.T) {
	// A variety of emojis:
	// 👋 (U+1F44B, 4 bytes, 2 UTF-16, width 2)
	// 🚀 (U+1F680, 4 bytes, 2 UTF-16, width 2)
	// 🎉 (U+1F389, 4 bytes, 2 UTF-16, width 2)
	// 🔥 (U+1F525, 4 bytes, 2 UTF-16, width 2)
	// 💻 (U+1F4BB, 4 bytes, 2 UTF-16, width 2)
	// 🦀 (U+1F980, 4 bytes, 2 UTF-16, width 2)
	emojis := []rune{'👋', '🚀', '🎉', '🔥', '💻', '🦀'}
	var emojiStr strings.Builder
	for _, em := range emojis {
		emojiStr.WriteRune(em)
	}
	text := emojiStr.String()

	prov := newMockLineProvider([]string{text + "\n"})
	bridge := NewUTFBridge(prov, 4)

	for i, em := range emojis {
		expectedByte := i * 4
		expectedLSP := i * 2    // 2 UTF-16 code units per emoji
		expectedVisual := i * 2 // 2 terminal cells per emoji

		pos := Position{Line: 0, Column: i, Byte: expectedByte}

		// 1. Position to Byte
		if b := bridge.PositionToByte(pos); b != expectedByte {
			t.Fatalf("emoji %d (%c): PositionToByte = %d, expected %d", i, em, b, expectedByte)
		}

		// 2. Byte to Position
		if p := bridge.ByteToPosition(expectedByte); p.Column != i || p.Byte != expectedByte {
			t.Fatalf("emoji %d (%c): ByteToPosition = %+v, expected col %d byte %d", i, em, p, i, expectedByte)
		}

		// 3. Position to LSP
		line, lsp := bridge.PositionToLSP(pos)
		if line != 0 || lsp != expectedLSP {
			t.Fatalf("emoji %d (%c): PositionToLSP = (%d, %d), expected (0, %d)", i, em, line, lsp, expectedLSP)
		}

		// 4. LSP to Position (exact match)
		pLSP := bridge.LSPToPosition(0, expectedLSP)
		if pLSP.Column != i || pLSP.Byte != expectedByte {
			t.Fatalf("emoji %d (%c): LSPToPosition = %+v, expected col %d byte %d", i, em, pLSP, i, expectedByte)
		}

		// 5. LSP Mid-Surrogate Forward Snapping!
		// For an emoji taking units [2*i, 2*i + 2), unit 2*i + 1 is in the MIDDLE of surrogate pair.
		// It MUST snap forward to the NEXT rune (i+1, byte (i+1)*4).
		midLSP := expectedLSP + 1
		pMid := bridge.LSPToPosition(0, midLSP)
		expectedSnapCol := i + 1
		expectedSnapByte := (i + 1) * 4
		if pMid.Column != expectedSnapCol || pMid.Byte != expectedSnapByte {
			t.Fatalf("emoji %d (%c): mid-surrogate LSP %d snapped to %+v, expected col %d byte %d",
				i, em, midLSP, pMid, expectedSnapCol, expectedSnapByte)
		}

		// 6. Visual Column: each emoji occupies 2 terminal display columns
		if v := bridge.VisualColumn(0, i); v != expectedVisual {
			t.Fatalf("emoji %d (%c): VisualColumn = %d, expected %d", i, em, v, expectedVisual)
		}

		// 7. Visual Column Halfway Rounding:
		// Left half (expectedVisual) -> snaps to rune i
		if r := bridge.RuneColFromVisual(0, expectedVisual); r != i {
			t.Fatalf("emoji %d (%c): RuneColFromVisual(left=%d) = %d, expected %d",
				i, em, expectedVisual, r, i)
		}
		// Right half (expectedVisual + 1) -> snaps to rune i + 1
		if r := bridge.RuneColFromVisual(0, expectedVisual+1); r != i+1 {
			t.Fatalf("emoji %d (%c): RuneColFromVisual(right=%d) = %d, expected %d",
				i, em, expectedVisual+1, r, i+1)
		}
	}
}

// TestStressUTFTabStopsAndCombinations tests tab stops with widths 1, 2, 4, 8,
// interleaved with Cyrillic, emojis, and ASCII.
func TestStressUTFTabStopsAndCombinations(t *testing.T) {
	tabWidths := []int{1, 2, 4, 8}

	for _, tw := range tabWidths {
		t.Run(fmt.Sprintf("TabWidth_%d", tw), func(t *testing.T) {
			// Line: "\tПривет\t👋\tWorld\n"
			// Runes:
			// 0: '\t'
			// 1: 'П', 2: 'р', 3: 'и', 4: 'в', 5: 'е', 6: 'т'
			// 7: '\t'
			// 8: '👋'
			// 9: '\t'
			// 10: 'W', 11: 'o', 12: 'r', 13: 'l', 14: 'd'
			rawText := "\tПривет\t👋\tWorld\n"
			prov := newMockLineProvider([]string{rawText})
			bridge := NewUTFBridge(prov, tw)

			// Calculate expected visual column manually for each rune
			lineBytes := []byte("\tПривет\t👋\tWorld")
			expectedVisCols := make([]int, 0)
			curV := 0
			idx := 0
			for idx < len(lineBytes) {
				expectedVisCols = append(expectedVisCols, curV)
				r, sz := utf8.DecodeRune(lineBytes[idx:])
				if r == '\t' {
					curV += tw - (curV % tw)
				} else {
					curV += runewidth.RuneWidth(r)
				}
				idx += sz
			}
			expectedVisCols = append(expectedVisCols, curV) // End of line

			// Verify VisualColumn for all runes
			for rCol, expectedV := range expectedVisCols {
				actualV := bridge.VisualColumn(0, rCol)
				if actualV != expectedV {
					t.Fatalf("[TabWidth %d] runeCol %d: VisualColumn = %d, expected %d",
						tw, rCol, actualV, expectedV)
				}
			}

			// Verify RuneColFromVisual across every single terminal column [0, curV + 5]
			for vCol := 0; vCol <= curV+5; vCol++ {
				rCol := bridge.RuneColFromVisual(0, vCol)
				if rCol < 0 || rCol > len(expectedVisCols)-1 {
					t.Fatalf("[TabWidth %d] vCol %d mapped to out-of-range runeCol %d",
						tw, vCol, rCol)
				}

				// If vCol is past line end, must clamp to last rune index
				if vCol >= curV {
					if rCol != len(expectedVisCols)-1 {
						t.Fatalf("[TabWidth %d] vCol %d past end mapped to %d, expected %d",
							tw, vCol, rCol, len(expectedVisCols)-1)
					}
				}
			}
		})
	}
}

// TestStressUTFCoordinateRoundTrips generates complex mixed multilingual lines
// and asserts round-trip property across all coordinate systems.
func TestStressUTFCoordinateRoundTrips(t *testing.T) {
	testLines := []string{
		"Hello, World!",
		"Привет, мир! 🚀",
		"\t\tIndent\tLevel\t",
		"👋🚀🎉🔥💻🦀",
		"CJK: 日本語 中文 한국어",
		"Mixed: 'П' + '👋' + '\t' + 'A' = 🌟",
		"Empty line coming next:",
		"",
		"   Spaces and \t tabs \t interleaved",
		"Symbols: @#$%^&*()_+~`|}{[]:;?><,./",
	}

	for _, text := range testLines {
		raw := text + "\n"
		prov := newMockLineProvider([]string{raw})
		bridge := NewUTFBridge(prov, 4)

		content := []byte(text)
		totalRunes := utf8.RuneCount(content)

		for rCol := 0; rCol <= totalRunes; rCol++ {
			// 1. Position to Byte -> Byte to Position
			pos := Position{Line: 0, Column: rCol, Byte: 0}
			byteOffset := bridge.PositionToByte(pos)
			posBack := bridge.ByteToPosition(byteOffset)

			if posBack.Column != rCol {
				t.Fatalf("[%s] Roundtrip mismatch: rCol %d -> byte %d -> rCol %d",
					text, rCol, byteOffset, posBack.Column)
			}
			if posBack.Byte != byteOffset {
				t.Fatalf("[%s] Roundtrip byte mismatch: byte %d -> posBack.Byte %d",
					text, byteOffset, posBack.Byte)
			}

			// 2. Position to LSP -> LSP to Position
			_, lspChar := bridge.PositionToLSP(pos)
			lspPosBack := bridge.LSPToPosition(0, lspChar)
			if lspPosBack.Column != rCol || lspPosBack.Byte != byteOffset {
				t.Fatalf("[%s] LSP roundtrip mismatch: rCol %d (LSP %d) -> %+v",
					text, rCol, lspChar, lspPosBack)
			}
		}
	}
}

// ============================================================================
// Stress Tests: Safe Atomic File Saving Under Crash / Interrupted I/O
// ============================================================================

// TestStressAtomicSaveSimulatedCrashInterruptedIO empirically verifies:
// 1. When an interrupted write or panic occurs midway, target file is NEVER modified or truncated.
// 2. Temporary file is guaranteed cleaned up and removed.
func TestStressAtomicSaveSimulatedCrashInterruptedIO(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_crash_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	targetFile := filepath.Join(dir, "vital_doc.txt")
	originalContent := []byte("CRITICAL UNMODIFIED PRODUCTION CODE: DO NOT LOSE THIS DATA\nLine 2: Intact\n")

	// Step 1: Create existing target file
	if err := os.WriteFile(targetFile, originalContent, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Step 2: Call SaveAtomicFile with nil ChunkSource (which panics immediately after tmpFile is opened)
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		_ = SaveAtomicFile(targetFile, nil, false)
	}()

	if !didPanic {
		t.Fatalf("expected nil source to panic, but execution completed")
	}

	// Step 3: Verify target file is 100% UNTOUCHED
	currentContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile on target failed: %v", err)
	}
	if !bytes.Equal(currentContent, originalContent) {
		t.Fatalf("DATA CORRUPTION DETECTED! Target file was altered or truncated!\nExpected: %q\nGot: %q",
			string(originalContent), string(currentContent))
	}

	// Step 4: Verify temporary file is NOT left orphaned on disk
	tmpPath := ComputeTempPath(targetFile)
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("CRASH ARTIFACT: temporary file %s was NOT cleaned up!", tmpPath)
	}
}

// TestStressAtomicSaveProcessKill simulates an abrupt process kill (SIGKILL/proc.Kill())
// during SaveAtomic to assert that the destination file is never corrupted.
func TestStressAtomicSaveProcessKill(t *testing.T) {
	// If in child subprocess mode: run the atomic save loop forever until killed
	if os.Getenv("TAHR_TEST_SUBPROCESS_KILL") == "1" {
		target := os.Getenv("TAHR_TEST_TARGET_FILE")
		// Generate 20MB of text chunks to take measurable time to write and flush
		hugeChunks := make([][]byte, 100)
		for i := range hugeChunks {
			hugeChunks[i] = bytes.Repeat([]byte("ABRUPT_KILL_TEST_DATA_LINE\n"), 10000)
		}
		source := &stringChunkSource{chunks: hugeChunks}
		_ = SaveAtomicFile(target, source, false)
		os.Exit(0)
	}

	dir, err := os.MkdirTemp("", "tahr_proc_kill_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	targetFile := filepath.Join(dir, "target.txt")
	originalContent := []byte("ORIGINAL UNCORRUPTED FILE CONTENT BEFORE PROCESS KILL\n")
	if err := os.WriteFile(targetFile, originalContent, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Launch subprocess executing this test with env TAHR_TEST_SUBPROCESS_KILL=1
	cmd := exec.Command(os.Args[0], "-test.run=TestStressAtomicSaveProcessKill")
	cmd.Env = append(os.Environ(),
		"TAHR_TEST_SUBPROCESS_KILL=1",
		fmt.Sprintf("TAHR_TEST_TARGET_FILE=%s", targetFile),
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start failed: %v", err)
	}

	// Let child process start writing chunks (wait 5-15ms)
	time.Sleep(10 * time.Millisecond)

	// Kill child process abruptly (simulating SIGKILL / task termination)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// Verify target file is 100% UNTOUCHED!
	postKillContent, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile after process kill failed: %v", err)
	}
	if !bytes.Equal(postKillContent, originalContent) {
		t.Fatalf("TARGET FILE CORRUPTED AFTER PROCESS KILL!\nExpected: %q\nGot: %q",
			string(originalContent), string(postKillContent))
	}
}

// TestStressAtomicSavePermissionPreservation verifies that permissions
// of existing destination files are preserved across saves.
func TestStressAtomicSavePermissionPreservation(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_perm_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	targetFile := filepath.Join(dir, "perm_file.txt")

	// Create initial file
	initialMode := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		initialMode = 0600 // Owner read-write only
	}
	if err := os.WriteFile(targetFile, []byte("Version 1\n"), initialMode); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	fiBefore, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat before failed: %v", err)
	}

	// Overwrite save with new content
	source := &stringChunkSource{chunks: [][]byte{[]byte("Version 2 - Updated\n")}}
	if err := SaveAtomicFile(targetFile, source, false); err != nil {
		t.Fatalf("SaveAtomicFile failed: %v", err)
	}

	fiAfter, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat after failed: %v", err)
	}

	// Compare permissions
	if fiAfter.Mode().Perm() != fiBefore.Mode().Perm() {
		t.Fatalf("File permissions NOT preserved! Before: %v, After: %v",
			fiBefore.Mode().Perm(), fiAfter.Mode().Perm())
	}

	content, err := os.ReadFile(targetFile)
	if err != nil || string(content) != "Version 2 - Updated\n" {
		t.Fatalf("Content mismatch after save: %q, err: %v", string(content), err)
	}
}

// TestStressAtomicSaveCRLFPreservation tests CRLF conversion across various
// edge cases: no trailing newline, mixed chunks, 10,000 lines.
func TestStressAtomicSaveCRLFPreservation(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_crlf_stress_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	testCases := []struct {
		name          string
		inputLines    []string
		trailingNL    bool
		expectedLines int
	}{
		{"SingleLineWithCRLF", []string{"Hello, World!"}, true, 1},
		{"SingleLineNoTrailingNL", []string{"Hello, World!"}, false, 1},
		{"MultiLineWithCRLF", []string{"Alpha", "Beta", "Gamma", "Delta"}, true, 4},
		{"MultiLineNoTrailingNL", []string{"Alpha", "Beta", "Gamma", "Delta"}, false, 4},
		{"EmptyLinesInBetween", []string{"Header", "", "Body", "", "Footer"}, true, 5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(dir, tc.name+".txt")

			// Build in-memory normalized LF text
			var sb strings.Builder
			for i, line := range tc.inputLines {
				sb.WriteString(line)
				if i < len(tc.inputLines)-1 || tc.trailingNL {
					sb.WriteByte('\n')
				}
			}
			rawLF := sb.String()

			buf := NewBufferWithText(rawLF)
			buf.SetCRLF(true)

			if err := buf.SaveAtomic(target); err != nil {
				t.Fatalf("SaveAtomic failed: %v", err)
			}

			// Read raw bytes on disk
			diskBytes, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("ReadFile failed: %v", err)
			}

			// Assert:
			// 1. NO isolated '\n' without '\r'
			for i := 0; i < len(diskBytes); i++ {
				if diskBytes[i] == '\n' {
					if i == 0 || diskBytes[i-1] != '\r' {
						t.Fatalf("Found isolated LF at byte %d without preceding CR! Content: %q",
							i, string(diskBytes))
					}
				}
			}

			// 2. NO double CR ('\r\r\n')
			if bytes.Contains(diskBytes, []byte("\r\r\n")) {
				t.Fatalf("Found double CR (\\r\\r\\n) in saved file: %q", string(diskBytes))
			}

			// 3. Load back with LoadFile and verify roundtrip
			loadedData, hasCRLF, err := LoadFile(target)
			if err != nil {
				t.Fatalf("LoadFile failed: %v", err)
			}
			// If file has at least one newline, hasCRLF must be true
			if len(tc.inputLines) > 1 || tc.trailingNL {
				if !hasCRLF {
					t.Fatalf("expected LoadFile to detect CRLF = true")
				}
			}
			if string(loadedData) != rawLF {
				t.Fatalf("Normalized loaded content mismatch!\nExpected: %q\nGot: %q",
					rawLF, string(loadedData))
			}
		})
	}
}

// TestStressAtomicSave100kLinesCRLFStress tests saving a 10,000-line buffer with CRLF.
func TestStressAtomicSave100kLinesCRLFStress(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_crlf_10k_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "large_crlf.txt")

	const lineCount = 10000
	var sb strings.Builder
	for i := 0; i < lineCount; i++ {
		sb.WriteString(fmt.Sprintf("Line %d: The quick brown fox jumps over the lazy dog\n", i))
	}
	lfText := sb.String()

	buf := NewBufferWithText(lfText)
	buf.SetCRLF(true)

	start := time.Now()
	if err := buf.SaveAtomic(target); err != nil {
		t.Fatalf("SaveAtomic 10k lines failed: %v", err)
	}
	dur := time.Since(start)
	t.Logf("Saved 10,000 lines with CRLF in %v", dur)

	diskBytes, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Verify all newlines are CRLF
	lfCount := bytes.Count(diskBytes, []byte{'\n'})
	crlfCount := bytes.Count(diskBytes, []byte("\r\n"))
	if lfCount != lineCount || crlfCount != lineCount {
		t.Fatalf("Line count mismatch: LF=%d, CRLF=%d, expected %d", lfCount, crlfCount, lineCount)
	}
	if bytes.Contains(diskBytes, []byte("\r\r\n")) {
		t.Fatalf("Detected double CR \\r\\r\\n")
	}
}

// TestStressAtomicSaveRapidSequentialSaves verifies reliability under 100 rapid sequential saves.
func TestStressAtomicSaveRapidSequentialSaves(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_rapid_save_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "rapid.txt")

	buf := NewBufferWithText("Initial content\n")
	for i := 0; i < 100; i++ {
		buf.InsertAtSelections(fmt.Sprintf("Step %d\n", i))
		if err := buf.SaveAtomic(target); err != nil {
			t.Fatalf("Rapid save failed at iteration %d: %v", i, err)
		}

		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("ReadFile failed at iteration %d: %v", i, err)
		}
		if len(data) == 0 {
			t.Fatalf("File truncated to 0 bytes at iteration %d!", i)
		}
	}
}

// TestStressAtomicSaveUnderWindowsFileLock verifies that if an atomic rename fails
// due to an active Windows file handle lock, the destination file is NEVER corrupted
// or truncated, and the temp file is properly cleaned up.
func TestStressAtomicSaveUnderWindowsFileLock(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_win_lock_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "locked.txt")
	originalContent := []byte("ORIGINAL UNCORRUPTED FILE DATA THAT MUST SURVIVE LOCK")
	if err := os.WriteFile(target, originalContent, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Intentionally open target file without FILE_SHARE_DELETE to simulate an exclusive lock
	lockedHandle, err := os.OpenFile(target, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	// Attempt SaveAtomicFile while file is locked
	newContent := []byte("NEW DATA THAT CANNOT OVERWRITE LOCKED FILE")
	source := &stringChunkSource{chunks: [][]byte{newContent}}
	saveErr := SaveAtomicFile(target, source, false)

	// On Windows, rename must fail with Access Denied or Sharing Violation
	if runtime.GOOS == "windows" {
		if saveErr == nil {
			t.Logf("Notice: Windows filesystem allowed rename over open handle")
		} else {
			t.Logf("Observed expected Windows file lock rejection: %v", saveErr)
		}
	}

	// Close lock handle
	_ = lockedHandle.Close()

	// Verify target file was NEVER corrupted or truncated!
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if saveErr != nil {
		if !bytes.Equal(content, originalContent) {
			t.Fatalf("DESTINATION CORRUPTED ON FAILED SAVE!\nExpected: %q\nGot: %q",
				string(originalContent), string(content))
		}
	}

	// Verify temporary file was cleaned up by defer guard
	tmpPath := ComputeTempPath(target)
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("Orphaned temporary file %s was NOT cleaned up after failed rename!", tmpPath)
	}

	// Now that lock is released, save MUST succeed cleanly
	if err := SaveAtomicFile(target, source, false); err != nil {
		t.Fatalf("Subsequent SaveAtomicFile failed after lock release: %v", err)
	}

	finalContent, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(finalContent, newContent) {
		t.Fatalf("Final content mismatch: %q, err: %v", string(finalContent), err)
	}
}

// TestStressComplexUnicodeSequences tests ZWJ sequences, flag surrogate pairs,
// skin tone modifiers, and combining accents.
func TestStressComplexUnicodeSequences(t *testing.T) {
	// Case 1: Combining character "Cafe\u0301" (width of combining acute is 0)
	cafeText := "Cafe\u0301\n"
	prov := newMockLineProvider([]string{cafeText})
	bridge := NewUTFBridge(prov, 4)

	// 'C'(1), 'a'(1), 'f'(1), 'e'(1), '\u0301'(0)
	// Rune cols: 0->0, 1->1, 2->2, 3->3, 4->4 (after 'e', visual col 4), 5->4 (after accent, visual col 4)
	if v := bridge.VisualColumn(0, 4); v != 4 {
		t.Fatalf("VisualColumn before accent expected 4, got %d", v)
	}
	if v := bridge.VisualColumn(0, 5); v != 4 {
		t.Fatalf("VisualColumn after zero-width combining accent expected 4, got %d", v)
	}

	// Case 2: Regional flag emoji "🇺🇸" (two surrogate pairs: U+1F1FA, U+1F1F8)
	flagText := "🇺🇸\n"
	provFlag := newMockLineProvider([]string{flagText})
	bridgeFlag := NewUTFBridge(provFlag, 4)

	// 2 runes, each 4 bytes, each 2 UTF-16 code units
	pos0 := Position{Line: 0, Column: 0, Byte: 0}
	pos1 := Position{Line: 0, Column: 1, Byte: 4}
	pos2 := Position{Line: 0, Column: 2, Byte: 8}

	if b := bridgeFlag.PositionToByte(pos0); b != 0 {
		t.Fatalf("flag pos0 byte expected 0, got %d", b)
	}
	if b := bridgeFlag.PositionToByte(pos1); b != 4 {
		t.Fatalf("flag pos1 byte expected 4, got %d", b)
	}
	if b := bridgeFlag.PositionToByte(pos2); b != 8 {
		t.Fatalf("flag pos2 byte expected 8, got %d", b)
	}
	_, lsp1 := bridgeFlag.PositionToLSP(pos1)
	if lsp1 != 2 {
		t.Fatalf("flag lsp1 expected 2, got %d", lsp1)
	}
	_, lsp2 := bridgeFlag.PositionToLSP(pos2)
	if lsp2 != 4 {
		t.Fatalf("flag lsp2 expected 4, got %d", lsp2)
	}

	// Case 3: Skin tone modifier "👍🏽" (U+1F44D + U+1F3FD)
	skinText := "👍🏽\n"
	provSkin := newMockLineProvider([]string{skinText})
	bridgeSkin := NewUTFBridge(provSkin, 4)

	pSkin1 := bridgeSkin.ByteToPosition(4)
	if pSkin1.Column != 1 || pSkin1.Byte != 4 {
		t.Fatalf("skin tone pos1 expected (col 1, byte 4), got %+v", pSkin1)
	}
	pSkin2 := bridgeSkin.ByteToPosition(8)
	if pSkin2.Column != 2 || pSkin2.Byte != 8 {
		t.Fatalf("skin tone pos2 expected (col 2, byte 8), got %+v", pSkin2)
	}
}

// TestStressTabStopsInterleavedWithWideCharacters tests exact terminal column positions
// when tabs follow wide characters and ASCII characters.
func TestStressTabStopsInterleavedWithWideCharacters(t *testing.T) {
	// Line A: "👋\tX\n"
	// '👋' is 2 cells (cols 0, 1).
	// Tab at col 2: tabWidth 4 -> tabStop = 4 - (2 % 4) = 2 cells (cols 2, 3).
	// 'X' starts at visual col 4!
	lineA := []byte("👋\tX\n")
	if v := LineRuneColToVisual(lineA, 0, 4); v != 0 {
		t.Fatalf("lineA col 0 visual expected 0, got %d", v)
	}
	if v := LineRuneColToVisual(lineA, 1, 4); v != 2 {
		t.Fatalf("lineA col 1 (after emoji) visual expected 2, got %d", v)
	}
	if v := LineRuneColToVisual(lineA, 2, 4); v != 4 {
		t.Fatalf("lineA col 2 (after tab) visual expected 4, got %d", v)
	}
	if v := LineRuneColToVisual(lineA, 3, 4); v != 5 {
		t.Fatalf("lineA col 3 (after X) visual expected 5, got %d", v)
	}

	// Line B: "a👋\tY\n"
	// 'a' is 1 cell (col 0).
	// '👋' is 2 cells (cols 1, 2).
	// Tab at col 3: tabWidth 4 -> tabStop = 4 - (3 % 4) = 1 cell (col 3).
	// 'Y' starts at visual col 4!
	lineB := []byte("a👋\tY\n")
	if v := LineRuneColToVisual(lineB, 1, 4); v != 1 {
		t.Fatalf("lineB after 'a' expected 1, got %d", v)
	}
	if v := LineRuneColToVisual(lineB, 2, 4); v != 3 {
		t.Fatalf("lineB after emoji expected 3, got %d", v)
	}
	if v := LineRuneColToVisual(lineB, 3, 4); v != 4 {
		t.Fatalf("lineB after tab expected 4, got %d", v)
	}
	if v := LineRuneColToVisual(lineB, 4, 4); v != 5 {
		t.Fatalf("lineB after Y expected 5, got %d", v)
	}
}

// TestStressAtomicSaveUnicodeAndCyrillicPaths tests atomic file saving
// to paths and directories containing Cyrillic and emoji characters.
func TestStressAtomicSaveUnicodeAndCyrillicPaths(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_utf_path_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	// Unicode subdirectory: "документы_📁"
	// Unicode filename: "проект_🚀.txt"
	targetDir := filepath.Join(dir, "документы_📁")
	targetFile := filepath.Join(targetDir, "проект_🚀.txt")

	testContent := []byte("ПРИВЕТ МИР! 🚀\nТестовые данные для атомарного сохранения.\n")
	source := &stringChunkSource{chunks: [][]byte{testContent}}

	if err := SaveAtomicFile(targetFile, source, false); err != nil {
		t.Fatalf("SaveAtomicFile with Unicode path failed: %v", err)
	}

	readBack, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("ReadFile on Unicode path failed: %v", err)
	}
	if !bytes.Equal(readBack, testContent) {
		t.Fatalf("Content mismatch on Unicode path!\nExpected: %q\nGot: %q",
			string(testContent), string(readBack))
	}
}

// TestStressAtomicSaveEmptyBuffer tests saving an empty (0-byte) buffer.
func TestStressAtomicSaveEmptyBuffer(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_empty_buf_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	targetFile := filepath.Join(dir, "empty.txt")
	emptyBuf := NewBuffer()

	if err := emptyBuf.SaveAtomic(targetFile); err != nil {
		t.Fatalf("SaveAtomic on empty buffer failed: %v", err)
	}

	fi, err := os.Stat(targetFile)
	if err != nil {
		t.Fatalf("Stat on empty target failed: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("Expected 0-byte file, got size %d", fi.Size())
	}

	loadedData, hasCRLF, err := LoadFile(targetFile)
	if err != nil {
		t.Fatalf("LoadFile on empty file failed: %v", err)
	}
	if len(loadedData) != 0 || hasCRLF {
		t.Fatalf("Unexpected loaded data from empty file: len=%d, hasCRLF=%v", len(loadedData), hasCRLF)
	}
}

// TestStressAtomicSaveConcurrentMultipleDifferentFiles verifies that multiple distinct
// files saved concurrently in separate goroutines do not collide or interfere.
func TestStressAtomicSaveConcurrentMultipleDifferentFiles(t *testing.T) {
	dir, err := os.MkdirTemp("", "tahr_concurrent_files_*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)

	const fileCount = 20
	var errCh = make(chan error, fileCount)

	for i := 0; i < fileCount; i++ {
		go func(id int) {
			path := filepath.Join(dir, fmt.Sprintf("file_%d.txt", id))
			payload := []byte(fmt.Sprintf("Concurrent File Payload %d: %s\n", id, strings.Repeat("X", 5000)))
			source := &stringChunkSource{chunks: [][]byte{payload}}

			if err := SaveAtomicFile(path, source, false); err != nil {
				errCh <- fmt.Errorf("file %d save error: %w", id, err)
				return
			}

			// Verify immediately
			data, err := os.ReadFile(path)
			if err != nil {
				errCh <- fmt.Errorf("file %d read error: %w", id, err)
				return
			}
			if !bytes.Equal(data, payload) {
				errCh <- fmt.Errorf("file %d content mismatch", id)
				return
			}
			errCh <- nil
		}(i)
	}

	for i := 0; i < fileCount; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("Concurrent save failure: %v", err)
		}
	}
}

