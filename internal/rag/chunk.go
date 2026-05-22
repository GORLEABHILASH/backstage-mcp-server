package rag

import (
	"strings"
	"unicode/utf8"
)

// ChunkOptions controls how text is split for embedding.
type ChunkOptions struct {
	Size    int // target characters per chunk
	Overlap int // characters of overlap between adjacent chunks
}

// DefaultChunkOptions returns sensible defaults (≈1000 chars, 200 char overlap).
// These values empirically work well for general technical documentation and
// fit comfortably under the 8192-token limit of text-embedding-3-small.
func DefaultChunkOptions() ChunkOptions {
	return ChunkOptions{Size: 1000, Overlap: 200}
}

// Chunk splits text into overlapping windows by character count, preferring
// paragraph boundaries when one is available inside the trailing slice of a
// window. Empty input returns nil.
func Chunk(text string, opts ChunkOptions) []string {
	if opts.Size <= 0 {
		opts = DefaultChunkOptions()
	}
	if opts.Overlap < 0 || opts.Overlap >= opts.Size {
		opts.Overlap = 0
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= opts.Size {
		return []string{text}
	}

	var chunks []string
	step := opts.Size - opts.Overlap
	for start := 0; start < len(runes); start += step {
		end := start + opts.Size
		if end >= len(runes) {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}
		// Try to break on a paragraph or sentence boundary in the last 20%.
		breakAt := findBoundary(runes, start, end)
		chunks = append(chunks, strings.TrimSpace(string(runes[start:breakAt])))
		// advance start to (breakAt - overlap); next iteration adds step but
		// we want overlap relative to breakAt, so adjust start manually
		next := breakAt - opts.Overlap
		if next <= start {
			next = start + step
		}
		start = next - step // compensate for the loop's += step
	}
	return chunks
}

// findBoundary returns an index in (start, end] preferring (in order) a
// blank-line break, a single newline, a sentence end, or end itself.
func findBoundary(runes []rune, start, end int) int {
	windowStart := start + (end-start)*4/5 // last 20% of the window
	if windowStart < start {
		windowStart = start
	}
	// blank-line
	if i := lastDoubleNewline(runes, windowStart, end); i > 0 {
		return i
	}
	// single newline
	if i := lastRune(runes, windowStart, end, '\n'); i > 0 {
		return i
	}
	// sentence end
	for _, r := range []rune{'.', '!', '?'} {
		if i := lastRune(runes, windowStart, end, r); i > 0 {
			return i + 1
		}
	}
	return end
}

func lastDoubleNewline(runes []rune, lo, hi int) int {
	for i := hi - 1; i > lo; i-- {
		if runes[i] == '\n' && runes[i-1] == '\n' {
			return i + 1
		}
	}
	return -1
}

func lastRune(runes []rune, lo, hi int, target rune) int {
	for i := hi - 1; i > lo; i-- {
		if runes[i] == target {
			return i + 1
		}
	}
	return -1
}

// estimateTokens is a rough heuristic (~4 chars/token for English) used to
// guard against sending payloads beyond the embedding model's context.
func estimateTokens(s string) int {
	return utf8.RuneCountInString(s) / 4
}
