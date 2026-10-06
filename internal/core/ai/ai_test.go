package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompletionCache(t *testing.T) {
	cache := NewCompletionCache(3)
	if cache.Len() != 0 {
		t.Fatalf("expected empty cache, got %d", cache.Len())
	}

	cache.Put("p1", "s1", "comp1")
	cache.Put("p2", "s2", "comp2")
	cache.Put("p3", "s3", "comp3")

	if v, ok := cache.Get("p1", "s1"); !ok || v != "comp1" {
		t.Errorf("expected comp1, got %s (ok=%v)", v, ok)
	}

	// Exceed capacity
	cache.Put("p4", "s4", "comp4")
	if cache.Len() != 3 {
		t.Errorf("expected cache length 3, got %d", cache.Len())
	}

	// p1 should have been evicted (oldest)
	if _, ok := cache.Get("p1", "s1"); ok {
		t.Errorf("expected p1 to be evicted")
	}

	// Clear
	cache.Clear()
	if cache.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", cache.Len())
	}
}

func TestCompletionCacheConcurrency(t *testing.T) {
	cache := NewCompletionCache(50)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p := "pref" + strconv.Itoa(id)
			cache.Put(p, "suf", "val"+strconv.Itoa(id))
			_, _ = cache.Get(p, "suf")
		}(i)
	}
	wg.Wait()
}

func TestBuildFIMPrompt(t *testing.T) {
	prompt := BuildFIMPrompt("def hello():", "    return True")
	if !strings.HasPrefix(prompt, FIMPrefixToken) {
		t.Errorf("expected prefix token at start, got %s", prompt)
	}
	if !strings.Contains(prompt, FIMSuffixToken) {
		t.Errorf("expected suffix token in prompt, got %s", prompt)
	}
	if !strings.HasSuffix(prompt, FIMMiddleToken) {
		t.Errorf("expected middle token at end, got %s", prompt)
	}
}

func TestCleanCompletion(t *testing.T) {
	// Single line cleaning
	raw := " fmt.Println(\"test\")\nfunc other() {}" + EndOfTextToken
	cleaned := CleanCompletion(raw, false)
	if cleaned != " fmt.Println(\"test\")" {
		t.Errorf("unexpected cleaned single-line: %q", cleaned)
	}

	// Multi line cleaning
	cleanedMulti := CleanCompletion(raw, true)
	if strings.Contains(cleanedMulti, EndOfTextToken) {
		t.Errorf("expected EndOfTextToken to be stripped")
	}
	if !strings.Contains(cleanedMulti, "func other() {}") {
		t.Errorf("expected multiline content preserved")
	}
}

func TestNextWordFromGhostText(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   foo", "   "},
		{"foo.Bar()", "foo"},
		{".Bar()", "."},
		{":= 42", ":="},
		{"(x, y)", "("},
		{"hello_world, ok", "hello_world"},
	}

	for _, c := range cases {
		got := NextWordFromGhostText(c.input)
		if got != c.expected {
			t.Errorf("NextWordFromGhostText(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestNormalizeProvider(t *testing.T) {
	if NormalizeProvider("llama-server") != ProviderBuiltin {
		t.Errorf("failed normalizing llama-server")
	}
	if NormalizeProvider("Ollama") != ProviderOllama {
		t.Errorf("failed normalizing Ollama")
	}
	if NormalizeProvider("OpenAI") != ProviderOpenAI {
		t.Errorf("failed normalizing OpenAI")
	}
	if NormalizeProvider("deepseek") != ProviderDeepSeek {
		t.Errorf("failed normalizing DeepSeek")
	}
	if NormalizeProvider("xyz") != ProviderCustom {
		t.Errorf("failed normalizing xyz")
	}
}

func TestEngineRequestCompletionOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"text": " := 10\nfmt.Println(x)"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.Provider = ProviderOpenAI
	cfg.Endpoint = ts.URL

	eng := NewEngine(cfg, "")
	defer eng.Stop()

	res, err := eng.RequestCompletion(context.Background(), "x", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != " := 10" {
		t.Errorf("expected ' := 10', got %q", res)
	}

	// Verify cached
	cached, ok := eng.cache.Get("x", "")
	if !ok || cached != " := 10" {
		t.Errorf("expected cached completion, got %q", cached)
	}
}

func TestEngineRequestCompletionOllama(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"response": "println(\"hello\")",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.Provider = ProviderOllama
	cfg.Endpoint = ts.URL

	eng := NewEngine(cfg, "")
	defer eng.Stop()

	res, err := eng.RequestCompletion(context.Background(), "fmt.", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "println(\"hello\")" {
		t.Errorf("expected println(\"hello\"), got %q", res)
	}
}

func TestEngineContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"text": "val"}}})
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	cfg.Provider = ProviderOpenAI
	cfg.Endpoint = ts.URL

	eng := NewEngine(cfg, "")
	defer eng.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := eng.RequestCompletion(ctx, "a", "b", false)
	if err == nil {
		t.Errorf("expected error due to canceled context, got nil")
	}
}

func TestEngineStatus(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Provider = ProviderBuiltin
	cfg.ModelPath = "nonexistent.gguf"
	cfg.LlamaServerPath = "nonexistent-server"

	eng := NewEngine(cfg, "")
	st, reason := eng.Status()
	if st != StatusNoModel && st != StatusNoServer {
		t.Errorf("expected No Model or No Server status, got %s (%s)", st, reason)
	}

	cfg.Enabled = false
	eng.UpdateConfig(cfg)
	st, _ = eng.Status()
	if st != StatusDisabled {
		t.Errorf("expected Disabled status, got %s", st)
	}
}

func TestDownloaderLifecycle(t *testing.T) {
	d := NewDownloader()
	if d.IsDownloading() {
		t.Errorf("expected IsDownloading to be false initially")
	}
	pct, msg := d.Progress()
	if pct != 0 || msg != "" {
		t.Errorf("unexpected initial progress: %d, %s", pct, msg)
	}

	d.setStatus(45, "Downloading model...")
	pct, msg = d.Progress()
	if pct != 45 || msg != "Downloading model..." {
		t.Errorf("expected 45 and message, got %d, %s", pct, msg)
	}
}

func TestEngineAutoDownloadCheck(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Provider = ProviderBuiltin
	cfg.ModelPath = filepath.Join(tmpDir, "models", "qwen.gguf")
	cfg.LlamaServerPath = filepath.Join(tmpDir, "bin", "llama-server")

	eng := NewEngine(cfg, tmpDir)

	// Since files are missing, CheckAndAutoDownload should recognize they need download
	var started atomic.Bool
	triggered := eng.CheckAndAutoDownload(tmpDir, func() {
		started.Store(true)
	}, nil, nil)

	if !triggered {
		t.Errorf("expected CheckAndAutoDownload to trigger download for missing files")
	}
	defer eng.downloader.Cancel()
}
