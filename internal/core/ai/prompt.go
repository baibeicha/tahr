package ai

import (
	"strings"
	"unicode"
)

// Fill-In-The-Middle (FIM) tokens for Qwen2.5-Coder and standard open LLMs.
const (
	FIMPrefixToken = "<|fim_prefix|>"
	FIMSuffixToken = "<|fim_suffix|>"
	FIMMiddleToken = "<|fim_middle|>"
	FIMPadToken    = "<|fim_pad|>"
	FIMRepoName    = "<|repo_name|>"
	FIMFileSep     = "<|file_separator|>"
	EndOfTextToken = "<|endoftext|>"
	ImEndToken     = "<|im_end|>"
)

// Standard stop tokens for inline code completion.
var (
	SingleLineStopTokens = []string{
		"\n",
		FIMPrefixToken,
		FIMSuffixToken,
		FIMMiddleToken,
		FIMFileSep,
		EndOfTextToken,
		ImEndToken,
	}

	MultiLineStopTokens = []string{
		FIMPrefixToken,
		FIMSuffixToken,
		FIMMiddleToken,
		FIMFileSep,
		EndOfTextToken,
		ImEndToken,
	}
)

// BuildFIMPrompt constructs a FIM-formatted prompt string for code completion.
// It limits prefix and suffix to maximum characters (e.g. 3000 chars) to maintain low latency.
func BuildFIMPrompt(prefix, suffix string) string {
	maxChars := 3000
	if len(prefix) > maxChars {
		prefix = prefix[len(prefix)-maxChars:]
	}
	if len(suffix) > maxChars {
		suffix = suffix[:maxChars]
	}
	return FIMPrefixToken + prefix + FIMSuffixToken + suffix + FIMMiddleToken
}

// CleanCompletion removes model artifacts, trailing end tokens, and trims the result.
func CleanCompletion(raw string, multiline bool) string {
	res := raw
	// Strip special tokens if returned in text
	tokensToStrip := []string{
		FIMPrefixToken, FIMSuffixToken, FIMMiddleToken,
		FIMFileSep, EndOfTextToken, ImEndToken,
	}
	for _, tok := range tokensToStrip {
		res = strings.ReplaceAll(res, tok, "")
	}

	if !multiline {
		if idx := strings.Index(res, "\n"); idx != -1 {
			res = res[:idx]
		}
	}
	// Do not trim leading spaces (indentation matters!), only trailing spaces
	return strings.TrimRight(res, " \r\t")
}

// NextWordFromGhostText extracts the next discrete chunk or word from a ghost text suggestion.
func NextWordFromGhostText(ghost string) string {
	if ghost == "" {
		return ""
	}
	runes := []rune(ghost)
	idx := 0

	// 1. If starts with whitespace, return all leading whitespace
	if unicode.IsSpace(runes[idx]) {
		for idx < len(runes) && unicode.IsSpace(runes[idx]) {
			idx++
		}
		return string(runes[:idx])
	}

	// 2. If starts with punctuation / symbols (e.g. `(`, `)`, `"`, `::`, `:=`, `==`)
	if isPunctuationOrSymbol(runes[idx]) {
		for idx < len(runes) && isPunctuationOrSymbol(runes[idx]) {
			idx++
		}
		return string(runes[:idx])
	}

	// 3. Otherwise alphanumeric identifier word (stops at punctuation or whitespace)
	for idx < len(runes) && (unicode.IsLetter(runes[idx]) || unicode.IsDigit(runes[idx]) || runes[idx] == '_') {
		idx++
	}

	return string(runes[:idx])
}

func isPunctuationOrSymbol(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) && r != '_'
}
