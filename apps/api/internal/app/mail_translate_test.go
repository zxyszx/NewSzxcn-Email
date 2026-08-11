package app

import (
	"context"
	"strings"
	"testing"
)

func TestParseGoogleTranslateResponse(t *testing.T) {
	raw := []any{
		[]any{
			[]any{"你好", "Hello", nil, nil, float64(3)},
			[]any{"，世界", ", world", nil, nil, float64(3)},
		},
		nil,
		"en",
	}
	translated, source := parseGoogleTranslateResponse(raw)
	if translated != "你好，世界" {
		t.Fatalf("translated = %q", translated)
	}
	if source != "en" {
		t.Fatalf("source = %q", source)
	}
}

func TestTranslateHTMLTextNodesWithPreservesMarkupAndSkipsCode(t *testing.T) {
	translator := func(_ context.Context, text, target string) (string, string, error) {
		return strings.ToUpper(text) + "-" + target, "en", nil
	}
	got, err := translateHTMLTextNodesWith(context.Background(), nil, `<p>Hello <strong>world</strong></p><pre>keep me</pre>`, "zh-CN", 100, translator)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<p>HELLO-zh-CN <strong>WORLD-zh-CN</strong></p>`) {
		t.Fatalf("translated HTML = %q", got)
	}
	if !strings.Contains(got, `<pre>keep me</pre>`) {
		t.Fatalf("code block was translated: %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	got, truncated := truncateRunes("你好world", 4)
	if got != "你好wo" || !truncated {
		t.Fatalf("truncateRunes() = %q, %v", got, truncated)
	}
	got, truncated = truncateRunes("你好", 4)
	if got != "你好" || truncated {
		t.Fatalf("truncateRunes() = %q, %v", got, truncated)
	}
}
