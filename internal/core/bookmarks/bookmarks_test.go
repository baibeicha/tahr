package bookmarks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBookmarkAddition(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-bm-add-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := NewStore(tmpDir)

	// 1. Add valid bookmark
	bm, err := store.AddBookmark("cmd/main.go", 42, "func main() {", "Entry point")
	if err != nil {
		t.Fatalf("unexpected error adding bookmark: %v", err)
	}
	if bm == nil || bm.ID == "" {
		t.Fatalf("expected bookmark with valid ID, got %+v", bm)
	}
	if bm.FilePath != "cmd/main.go" {
		t.Fatalf("expected FilePath 'cmd/main.go', got %q", bm.FilePath)
	}
	if bm.LineNumber != 42 {
		t.Fatalf("expected LineNumber 42, got %d", bm.LineNumber)
	}
	if bm.LinePreview != "func main() {" {
		t.Fatalf("expected LinePreview 'func main() {', got %q", bm.LinePreview)
	}
	if bm.Label != "Entry point" {
		t.Fatalf("expected Label 'Entry point', got %q", bm.Label)
	}
	if bm.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt timestamp")
	}

	// 2. Validate error conditions
	if _, err := store.AddBookmark("", 10, "foo", ""); err == nil {
		t.Fatal("expected error on empty file path, got nil")
	}
	if _, err := store.AddBookmark("file.go", 0, "foo", ""); err == nil {
		t.Fatal("expected error on line number < 1, got nil")
	}

	// 3. Update existing bookmark on same line
	bmUpdated, err := store.AddBookmark("cmd/main.go", 42, "func main() { // updated", "Updated Label")
	if err != nil {
		t.Fatalf("unexpected error updating bookmark: %v", err)
	}
	if bmUpdated.ID != bm.ID {
		t.Fatalf("expected same ID %s, got %s", bm.ID, bmUpdated.ID)
	}
	if bmUpdated.LinePreview != "func main() { // updated" {
		t.Fatalf("expected updated preview, got %q", bmUpdated.LinePreview)
	}
	if bmUpdated.Label != "Updated Label" {
		t.Fatalf("expected updated label, got %q", bmUpdated.Label)
	}
	if store.Count() != 1 {
		t.Fatalf("expected 1 bookmark, got %d", store.Count())
	}

	// 4. ToggleBookmark: Remove then Add
	toggledBm, added, err := store.ToggleBookmark("cmd/main.go", 42, "", "")
	if err != nil || added {
		t.Fatalf("expected toggle to remove existing bookmark, added=%v err=%v", added, err)
	}
	if toggledBm.ID != bm.ID {
		t.Fatalf("expected toggled bookmark ID %s, got %s", bm.ID, toggledBm.ID)
	}
	if store.Count() != 0 {
		t.Fatalf("expected 0 bookmarks after toggle removal, got %d", store.Count())
	}

	// Toggle again to add
	newBm, added, err := store.ToggleBookmark("cmd/main.go", 42, "func main() {", "Toggled in")
	if err != nil || !added {
		t.Fatalf("expected toggle to re-add bookmark, added=%v err=%v", added, err)
	}
	if store.Count() != 1 {
		t.Fatalf("expected 1 bookmark after re-adding, got %d", store.Count())
	}
	if newBm.Label != "Toggled in" {
		t.Fatalf("expected label 'Toggled in', got %q", newBm.Label)
	}

	// 5. Query helpers
	fetched := store.GetBookmark(newBm.ID)
	if fetched == nil || fetched.ID != newBm.ID {
		t.Fatalf("expected to get bookmark by ID %s", newBm.ID)
	}

	byLoc := store.GetBookmarkAt("cmd/main.go", 42)
	if byLoc == nil || byLoc.ID != newBm.ID {
		t.Fatalf("expected to get bookmark by file and line")
	}

	fileBms := store.GetBookmarksForFile("cmd/main.go")
	if len(fileBms) != 1 || fileBms[0].ID != newBm.ID {
		t.Fatalf("expected 1 file bookmark, got %d", len(fileBms))
	}
}

func TestBookmarkRemoval(t *testing.T) {
	store := NewStore("")

	bm1, _ := store.AddBookmark("pkg/server.go", 15, "srv := NewServer()", "Server init")
	bm2, _ := store.AddBookmark("pkg/server.go", 45, "srv.Start()", "Server start")
	bm3, _ := store.AddBookmark("pkg/client.go", 10, "c := NewClient()", "Client init")

	if store.Count() != 3 {
		t.Fatalf("expected 3 bookmarks, got %d", store.Count())
	}

	// Remove non-existent ID
	if store.RemoveBookmark("non_existent_id") {
		t.Fatal("expected false when removing non-existent bookmark ID")
	}

	// Remove by ID
	if !store.RemoveBookmark(bm2.ID) {
		t.Fatalf("failed to remove bookmark by ID %s", bm2.ID)
	}
	if store.Count() != 2 {
		t.Fatalf("expected 2 bookmarks, got %d", store.Count())
	}
	if store.GetBookmark(bm2.ID) != nil {
		t.Fatal("expected removed bookmark to be nil on lookup")
	}

	// Remove by Location
	if !store.RemoveBookmarkAt(bm3.FilePath, bm3.LineNumber) {
		t.Fatal("failed to remove bookmark at pkg/client.go:10")
	}
	if store.Count() != 1 {
		t.Fatalf("expected 1 bookmark, got %d", store.Count())
	}

	// ClearAll
	if err := store.ClearAll(); err != nil {
		t.Fatalf("failed to ClearAll: %v", err)
	}
	if store.Count() != 0 {
		t.Fatalf("expected 0 bookmarks after ClearAll, got %d", store.Count())
	}
	if store.GetBookmark(bm1.ID) != nil {
		t.Fatal("expected store to be completely empty after ClearAll")
	}
}

func TestBookmarkSerialization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tahr-bm-ser-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store1 := NewStore(tmpDir)

	bm1, err := store1.AddBookmark("src/core.go", 100, "type Core struct {}", "Core Struct")
	if err != nil {
		t.Fatalf("failed to add bookmark 1: %v", err)
	}
	bm2, err := store1.AddBookmark("src/parser.go", 25, "func ParseTokens()", "Lexer Entry")
	if err != nil {
		t.Fatalf("failed to add bookmark 2: %v", err)
	}

	// Save to <workspace>/.tahr/bookmarks.json
	if err := store1.Save(); err != nil {
		t.Fatalf("failed to save bookmarks: %v", err)
	}

	targetPath := filepath.Join(tmpDir, ".tahr", "bookmarks.json")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected bookmarks file to exist at %s: %v", targetPath, err)
	}

	// Load into a fresh store instance
	store2 := NewStore(tmpDir)
	if err := store2.Load(); err != nil {
		t.Fatalf("failed to load bookmarks: %v", err)
	}

	if store2.Count() != 2 {
		t.Fatalf("expected 2 loaded bookmarks, got %d", store2.Count())
	}

	loaded1 := store2.GetBookmark(bm1.ID)
	if loaded1 == nil {
		t.Fatalf("failed to retrieve loaded bookmark 1 by ID %s", bm1.ID)
	}
	if loaded1.FilePath != "src/core.go" || loaded1.LineNumber != 100 || loaded1.LinePreview != "type Core struct {}" || loaded1.Label != "Core Struct" {
		t.Fatalf("mismatched bookmark 1 fields: %+v", loaded1)
	}

	loaded2 := store2.GetBookmark(bm2.ID)
	if loaded2 == nil {
		t.Fatalf("failed to retrieve loaded bookmark 2 by ID %s", bm2.ID)
	}
	if loaded2.FilePath != "src/parser.go" || loaded2.LineNumber != 25 || loaded2.LinePreview != "func ParseTokens()" || loaded2.Label != "Lexer Entry" {
		t.Fatalf("mismatched bookmark 2 fields: %+v", loaded2)
	}

	// Test loading bare array JSON format for backwards compatibility
	bareFile := filepath.Join(tmpDir, ".tahr", "bare_bookmarks.json")
	bareData, err := json.Marshal([]*Bookmark{
		{
			ID:          "bm_bare_1",
			FilePath:    "pkg/bare.go",
			LineNumber:  77,
			LinePreview: "const MaxLimit = 100",
			Label:       "Constants",
			CreatedAt:   time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal bare array: %v", err)
	}
	if err := os.WriteFile(bareFile, bareData, 0644); err != nil {
		t.Fatalf("failed to write bare file: %v", err)
	}

	store3 := NewStore(tmpDir)
	store3.SetFilePath(bareFile)
	if err := store3.Load(); err != nil {
		t.Fatalf("failed to load bare array format: %v", err)
	}
	if store3.Count() != 1 {
		t.Fatalf("expected 1 bookmark from bare array, got %d", store3.Count())
	}
	b := store3.GetBookmark("bm_bare_1")
	if b == nil || b.LineNumber != 77 {
		t.Fatalf("unexpected bookmark from bare array: %+v", b)
	}
}

func TestCircularNavigationAcrossFiles(t *testing.T) {
	store := NewStore("")

	// 1. Empty store navigation returns nil
	nav := store.Navigator()
	if nav.NextBookmark("file.go", 10) != nil {
		t.Fatal("expected nil from NextBookmark on empty store")
	}
	if nav.PrevBookmark("file.go", 10) != nil {
		t.Fatal("expected nil from PrevBookmark on empty store")
	}

	// 2. Single bookmark in store returns itself circularly
	bmSingle, _ := store.AddBookmark("main.go", 42, "func main()", "Entry")
	if got := nav.NextBookmark("main.go", 42); got == nil || got.ID != bmSingle.ID {
		t.Fatalf("expected single bookmark on Next, got %+v", got)
	}
	if got := nav.PrevBookmark("main.go", 42); got == nil || got.ID != bmSingle.ID {
		t.Fatalf("expected single bookmark on Prev, got %+v", got)
	}

	// 3. Setup multiple bookmarks across multiple files
	_ = store.ClearAll()
	bmA10, _ := store.AddBookmark("a_file.go", 10, "line 10", "A10")
	bmA50, _ := store.AddBookmark("a_file.go", 50, "line 50", "A50")
	bmB20, _ := store.AddBookmark("b_file.go", 20, "line 20", "B20")
	bmC05, _ := store.AddBookmark("c_file.go", 5, "line 5", "C05")

	// Verify Sorted Order:
	// 0: a_file.go:10
	// 1: a_file.go:50
	// 2: b_file.go:20
	// 3: c_file.go:5

	// Forward Cycling (NextBookmark)
	step1 := nav.NextBookmark("a_file.go", 10)
	if step1.ID != bmA50.ID {
		t.Fatalf("step1: expected A50, got %s:%d", step1.FilePath, step1.LineNumber)
	}

	step2 := nav.NextBookmark("a_file.go", 50)
	if step2.ID != bmB20.ID {
		t.Fatalf("step2: expected B20, got %s:%d", step2.FilePath, step2.LineNumber)
	}

	step3 := nav.NextBookmark("b_file.go", 20)
	if step3.ID != bmC05.ID {
		t.Fatalf("step3: expected C05, got %s:%d", step3.FilePath, step3.LineNumber)
	}

	// Circular wrap around from last bookmark to first!
	step4 := nav.NextBookmark("c_file.go", 5)
	if step4.ID != bmA10.ID {
		t.Fatalf("step4 (wrap): expected A10, got %s:%d", step4.FilePath, step4.LineNumber)
	}

	// Backward Cycling (PrevBookmark)
	pStep1 := nav.PrevBookmark("c_file.go", 5)
	if pStep1.ID != bmB20.ID {
		t.Fatalf("pStep1: expected B20, got %s:%d", pStep1.FilePath, pStep1.LineNumber)
	}

	pStep2 := nav.PrevBookmark("b_file.go", 20)
	if pStep2.ID != bmA50.ID {
		t.Fatalf("pStep2: expected A50, got %s:%d", pStep2.FilePath, pStep2.LineNumber)
	}

	pStep3 := nav.PrevBookmark("a_file.go", 50)
	if pStep3.ID != bmA10.ID {
		t.Fatalf("pStep3: expected A10, got %s:%d", pStep3.FilePath, pStep3.LineNumber)
	}

	// Circular wrap around from first bookmark backwards to last!
	pStep4 := nav.PrevBookmark("a_file.go", 10)
	if pStep4.ID != bmC05.ID {
		t.Fatalf("pStep4 (wrap): expected C05, got %s:%d", pStep4.FilePath, pStep4.LineNumber)
	}

	// Intermediate cursor positions (between bookmarks)
	between1 := nav.NextBookmark("a_file.go", 30)
	if between1.ID != bmA50.ID {
		t.Fatalf("between1: expected A50, got %s:%d", between1.FilePath, between1.LineNumber)
	}

	betweenPrev := nav.PrevBookmark("a_file.go", 30)
	if betweenPrev.ID != bmA10.ID {
		t.Fatalf("betweenPrev: expected A10, got %s:%d", betweenPrev.FilePath, betweenPrev.LineNumber)
	}

	// Position before all bookmarks
	beforeAll := nav.NextBookmark("00_start.go", 1)
	if beforeAll.ID != bmA10.ID {
		t.Fatalf("beforeAll Next: expected A10, got %s:%d", beforeAll.FilePath, beforeAll.LineNumber)
	}
	beforeAllPrev := nav.PrevBookmark("00_start.go", 1)
	if beforeAllPrev.ID != bmC05.ID {
		t.Fatalf("beforeAll Prev (wrap): expected C05, got %s:%d", beforeAllPrev.FilePath, beforeAllPrev.LineNumber)
	}

	// Position after all bookmarks
	afterAll := nav.NextBookmark("zz_end.go", 100)
	if afterAll.ID != bmA10.ID {
		t.Fatalf("afterAll Next (wrap): expected A10, got %s:%d", afterAll.FilePath, afterAll.LineNumber)
	}
	afterAllPrev := nav.PrevBookmark("zz_end.go", 100)
	if afterAllPrev.ID != bmC05.ID {
		t.Fatalf("afterAll Prev: expected C05, got %s:%d", afterAllPrev.FilePath, afterAllPrev.LineNumber)
	}
}
