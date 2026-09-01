package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGoogleTranslateRequestUsesFormBody(t *testing.T) {
	text := strings.Repeat("长邮件正文", 2000)
	req, err := newGoogleTranslateRequest(context.Background(), text, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	if req.URL.RawQuery != "" {
		t.Fatalf("translation text leaked into URL query: %q", req.URL.RawQuery)
	}
	if got := req.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
		t.Fatalf("content type = %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("q") != text || values.Get("tl") != "zh-CN" || values.Get("sl") != "auto" {
		t.Fatalf("unexpected form values: q=%d runes tl=%q sl=%q", len([]rune(values.Get("q"))), values.Get("tl"), values.Get("sl"))
	}
}

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

func TestTranslateHTMLTextNodesBatchWithUsesSingleRequest(t *testing.T) {
	calls := 0
	translator := func(_ context.Context, text, target string) (string, string, error) {
		calls++
		if !strings.Contains(text, `data-newszxcn-segment="0"`) || !strings.Contains(text, `data-newszxcn-segment="1"`) {
			t.Fatalf("batched request missing segments: %q", text)
		}
		return strings.NewReplacer("Hello", "你好", "world", "世界").Replace(text), "en", nil
	}
	got, err := translateHTMLTextNodesBatchWith(context.Background(), nil, `<p>Hello <strong>world</strong></p><pre>keep me</pre>`, "zh-CN", 100, translator)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("translator calls = %d, want 1", calls)
	}
	if !strings.Contains(got, `<p>你好 <strong>世界</strong></p>`) {
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
