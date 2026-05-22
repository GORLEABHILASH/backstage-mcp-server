package rag

import (
	"strings"
	"testing"
)

func TestChunk_ShortInputReturnsSingle(t *testing.T) {
	got := Chunk("hello world", ChunkOptions{Size: 100, Overlap: 10})
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("unexpected chunks: %#v", got)
	}
}

func TestChunk_EmptyInputReturnsNil(t *testing.T) {
	if got := Chunk("   ", DefaultChunkOptions()); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestChunk_OverlapsBetweenWindows(t *testing.T) {
	// 500 'a's followed by 500 'b's, no boundary chars; ensure we produce
	// multiple chunks and that consecutive chunks share content (overlap).
	text := strings.Repeat("a", 500) + strings.Repeat("b", 500)
	chunks := Chunk(text, ChunkOptions{Size: 400, Overlap: 100})
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	tail := chunks[0][len(chunks[0])-50:]
	if !strings.Contains(chunks[1], tail) {
		t.Fatalf("expected overlap between chunks; chunk0 tail %q not found in chunk1", tail)
	}
}

func TestChunk_PrefersParagraphBoundary(t *testing.T) {
	// Build a string with a paragraph break in the last 20% of the window.
	body := strings.Repeat("x", 800) + "\n\n" + strings.Repeat("y", 800)
	chunks := Chunk(body, ChunkOptions{Size: 1000, Overlap: 50})
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	if strings.HasSuffix(strings.TrimSpace(chunks[0]), "y") {
		t.Fatalf("first chunk should not cross the paragraph break: %q", chunks[0][len(chunks[0])-20:])
	}
}
