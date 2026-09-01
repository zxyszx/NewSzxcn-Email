package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/html"
)

const googleTranslateEndpoint = "https://translate.googleapis.com/translate_a/single"

var googleTranslateEndpoints = []string{
	googleTranslateEndpoint,
	"https://translate.google.com/translate_a/single",
}

type translateMailMessageRequest struct {
	TargetLanguage string `json:"targetLanguage"`
}

type translateMailMessageResponse struct {
	TranslatedText string `json:"translatedText"`
	TranslatedHTML string `json:"translatedHtml,omitempty"`
	SourceLanguage string `json:"sourceLanguage,omitempty"`
	TargetLanguage string `json:"targetLanguage"`
	Truncated      bool   `json:"truncated"`
}

func (a *App) handleTranslateMailMessage(w http.ResponseWriter, r *http.Request) {
	if !a.config().MailTranslateEnabled {
		respondError(w, http.StatusForbidden, "mail translation is disabled")
		return
	}
	var req translateMailMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	target := normalizeTranslateTarget(req.TargetLanguage)
	if target == "" {
		respondError(w, http.StatusBadRequest, "unsupported target language")
		return
	}
	msg, err := a.loadMessageForRequest(r, chi.URLParam(r, "id"), true)
	if err != nil {
		respondError(w, http.StatusNotFound, "message not found")
		return
	}
	text := strings.TrimSpace(msg.BodyText)
	if text == "" {
		text = strings.TrimSpace(msg.Snippet)
	}
	if text == "" {
		respondError(w, http.StatusBadRequest, "message has no translatable text")
		return
	}
	maxChars := a.config().MailTranslateMaxChars
	if maxChars <= 0 {
		maxChars = 8000
	}
	text, truncated := truncateRunes(text, maxChars)
	translated, source, err := googleFreeTranslate(r.Context(), text, target)
	if err != nil {
		a.log.Warn("mail translation failed", "message_id", msg.ID, "target", target, "error", err)
		respondError(w, http.StatusBadGateway, "翻译服务暂时不可用，请稍后重试")
		return
	}
	translatedHTML := ""
	if strings.TrimSpace(msg.BodyHTML) != "" {
		translatedHTML, _ = translateHTMLTextNodes(r.Context(), a.policy, msg.BodyHTML, target, maxChars)
	}
	respondJSON(w, http.StatusOK, translateMailMessageResponse{TranslatedText: translated, TranslatedHTML: translatedHTML, SourceLanguage: source, TargetLanguage: target, Truncated: truncated})
}

func (a *App) handleTranslateExternalIMAPMessage(w http.ResponseWriter, r *http.Request) {
	if !a.config().MailTranslateEnabled {
		respondError(w, http.StatusForbidden, "mail translation is disabled")
		return
	}
	var req translateMailMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	target := normalizeTranslateTarget(req.TargetLanguage)
	if target == "" {
		respondError(w, http.StatusBadRequest, "unsupported target language")
		return
	}
	account, ok := a.externalIMAPAccountForMailRequest(w, r)
	if !ok {
		return
	}
	folder, uid, ok := decodeExternalRemoteID(w, chi.URLParam(r, "remoteId"))
	if !ok {
		return
	}
	client, err := a.externalIMAP.openExternalIMAPClient(r.Context(), account)
	if err != nil {
		respondError(w, http.StatusBadRequest, "connection failed: "+err.Error())
		return
	}
	defer client.Close()
	raw, remote, err := client.FetchRaw(r.Context(), folder, uid)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to load remote message")
		return
	}
	stored, _, err := a.parseMaildirMessage(raw, account.Username)
	text := ""
	if err == nil {
		text = strings.TrimSpace(stored.BodyText)
		if text == "" {
			text = strings.TrimSpace(stored.Snippet)
		}
	}
	if text == "" {
		text = strings.TrimSpace(remote.Snippet)
	}
	if text == "" {
		respondError(w, http.StatusBadRequest, "message has no translatable text")
		return
	}
	maxChars := a.config().MailTranslateMaxChars
	if maxChars <= 0 {
		maxChars = 8000
	}
	text, truncated := truncateRunes(text, maxChars)
	translated, source, err := googleFreeTranslate(r.Context(), text, target)
	if err != nil {
		a.log.Warn("external mail translation failed", "account_id", account.ID, "remote_id", chi.URLParam(r, "remoteId"), "target", target, "error", err)
		respondError(w, http.StatusBadGateway, "翻译服务暂时不可用，请稍后重试")
		return
	}
	translatedHTML := ""
	if err == nil && strings.TrimSpace(stored.BodyHTML) != "" {
		translatedHTML, _ = translateHTMLTextNodes(r.Context(), a.policy, stored.BodyHTML, target, maxChars)
	}
	respondJSON(w, http.StatusOK, translateMailMessageResponse{TranslatedText: translated, TranslatedHTML: translatedHTML, SourceLanguage: source, TargetLanguage: target, Truncated: truncated})
}

func translateHTMLTextNodes(ctx context.Context, policy *HTMLPolicy, bodyHTML, target string, maxChars int) (string, error) {
	return translateHTMLTextNodesBatchWith(ctx, policy, bodyHTML, target, maxChars, googleFreeTranslate)
}

type htmlTextTranslator func(context.Context, string, string) (string, string, error)

// translateHTMLTextNodesBatchWith sends all visible text nodes in one request.
// This preserves the original markup without flooding the translation provider
// with one request per HTML node.
func translateHTMLTextNodesBatchWith(ctx context.Context, policy *HTMLPolicy, bodyHTML, target string, maxChars int, translator htmlTextTranslator) (string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(bodyHTML), nil)
	if err != nil {
		return "", err
	}
	type translationJob struct {
		node *html.Node
		text string
	}
	remaining := maxChars
	jobs := make([]translationJob, 0)
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && shouldSkipHTMLTranslationElement(n.Data) {
			return
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" && containsTranslatableLetter(text) && remaining > 0 {
				limited, _ := truncateRunes(text, remaining)
				remaining -= utf8.RuneCountInString(limited)
				jobs = append(jobs, translationJob{node: n, text: limited})
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	for _, node := range nodes {
		collect(node)
	}
	if len(jobs) == 0 {
		return "", errors.New("html has no translatable text")
	}

	var requestBody strings.Builder
	for index, job := range jobs {
		fmt.Fprintf(&requestBody, `<p data-newszxcn-segment="%d">%s</p>`, index, html.EscapeString(job.text))
	}
	translatedBody, _, err := translator(ctx, requestBody.String(), target)
	if err != nil {
		return "", err
	}
	translatedNodes, err := html.ParseFragment(strings.NewReader(translatedBody), nil)
	if err != nil {
		return "", err
	}
	results := make([]string, len(jobs))
	var readResults func(*html.Node)
	readResults = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key != "data-newszxcn-segment" {
					continue
				}
				var index int
				if _, scanErr := fmt.Sscanf(attr.Val, "%d", &index); scanErr == nil && index >= 0 && index < len(results) {
					results[index] = strings.TrimSpace(htmlNodeText(n))
				}
				break
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			readResults(child)
		}
	}
	for _, node := range translatedNodes {
		readResults(node)
	}
	for index, result := range results {
		if result == "" {
			return "", fmt.Errorf("missing translated html segment %d", index)
		}
		jobs[index].node.Data = strings.Replace(jobs[index].node.Data, jobs[index].text, result, 1)
	}
	var rendered bytes.Buffer
	for _, node := range nodes {
		if err := html.Render(&rendered, node); err != nil {
			return "", err
		}
	}
	if policy != nil {
		return policy.Sanitize(rendered.String()), nil
	}
	return rendered.String(), nil
}

func htmlNodeText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func translateHTMLTextNodesWith(ctx context.Context, policy *HTMLPolicy, bodyHTML, target string, maxChars int, translator htmlTextTranslator) (string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(bodyHTML), nil)
	if err != nil {
		return "", err
	}
	type translationJob struct {
		node     *html.Node
		original string
		text     string
	}
	remaining := maxChars
	jobs := make([]translationJob, 0)
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && shouldSkipHTMLTranslationElement(n.Data) {
			return
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" && containsTranslatableLetter(text) && remaining > 0 {
				limited, _ := truncateRunes(text, remaining)
				remaining -= utf8.RuneCountInString(limited)
				jobs = append(jobs, translationJob{node: n, original: text, text: limited})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	for _, n := range nodes {
		collect(n)
	}
	results := make([]string, len(jobs))
	jobIndexes := make(chan int)
	errCh := make(chan error, 1)
	workers := min(4, len(jobs))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobIndexes {
				translated, _, translateErr := translator(ctx, jobs[index].text, target)
				if translateErr != nil {
					select {
					case errCh <- translateErr:
					default:
					}
					continue
				}
				results[index] = translated
			}
		}()
	}
	for index := range jobs {
		jobIndexes <- index
	}
	close(jobIndexes)
	wg.Wait()
	select {
	case translateErr := <-errCh:
		return "", translateErr
	default:
	}
	for index, job := range jobs {
		job.node.Data = strings.Replace(job.node.Data, job.original, results[index], 1)
	}
	var b bytes.Buffer
	for _, n := range nodes {
		if err := html.Render(&b, n); err != nil {
			return "", err
		}
	}
	if policy != nil {
		return policy.Sanitize(b.String()), nil
	}
	return b.String(), nil
}

func shouldSkipHTMLTranslationElement(tag string) bool {
	switch strings.ToLower(tag) {
	case "script", "style", "code", "pre", "textarea":
		return true
	default:
		return false
	}
}

func containsTranslatableLetter(value string) bool {
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '\u4e00' && r <= '\u9fff') {
			return true
		}
	}
	return false
}

func normalizeTranslateTarget(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "zh-cn", "zh-hans", "zh_cn":
		return "zh-CN"
	case "zh-tw", "zh-hant", "zh_hk", "zh-hk", "zh-mo":
		return "zh-TW"
	case "en", "en-us", "en-gb":
		return "en"
	default:
		return ""
	}
}

func truncateRunes(value string, max int) (string, bool) {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value, false
	}
	out := make([]rune, 0, max)
	for i, r := range value {
		if len(out) >= max {
			return string(out), i < len(value)
		}
		out = append(out, r)
	}
	return string(out), false
}

func googleFreeTranslate(ctx context.Context, text, target string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var lastErr error
	for endpointIndex, endpoint := range googleTranslateEndpoints {
		translated, source, err := googleTranslateOnce(ctx, endpoint, text, target)
		if err == nil {
			return translated, source, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		if endpointIndex < len(googleTranslateEndpoints)-1 {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", "", ctx.Err()
			case <-timer.C:
			}
		}
	}
	return "", "", lastErr
}

func googleTranslateOnce(ctx context.Context, endpoint, text, target string) (string, string, error) {
	req, err := newGoogleTranslateRequestForEndpoint(ctx, endpoint, text, target)
	if err != nil {
		return "", "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
		return "", "", fmt.Errorf("google translate status %d", res.StatusCode)
	}
	var raw any
	if err := json.NewDecoder(io.LimitReader(res.Body, 4*1024*1024)).Decode(&raw); err != nil {
		return "", "", err
	}
	translated, source := parseGoogleTranslateResponse(raw)
	translated = strings.TrimSpace(translated)
	if translated == "" {
		return "", source, errors.New("empty translation")
	}
	return translated, source, nil
}

func newGoogleTranslateRequest(ctx context.Context, text, target string) (*http.Request, error) {
	return newGoogleTranslateRequestForEndpoint(ctx, googleTranslateEndpoint, text, target)
}

func newGoogleTranslateRequestForEndpoint(ctx context.Context, endpoint, text, target string) (*http.Request, error) {
	params := url.Values{}
	params.Set("client", "gtx")
	params.Set("sl", "auto")
	params.Set("tl", target)
	params.Set("dt", "t")
	params.Set("q", text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	return req, nil
}

func parseGoogleTranslateResponse(raw any) (string, string) {
	root, _ := raw.([]any)
	var b strings.Builder
	if len(root) > 0 {
		if sentences, ok := root[0].([]any); ok {
			for _, item := range sentences {
				parts, ok := item.([]any)
				if !ok || len(parts) == 0 {
					continue
				}
				if s, ok := parts[0].(string); ok {
					b.WriteString(s)
				}
			}
		}
	}
	source := ""
	if len(root) > 2 {
		if s, ok := root[2].(string); ok {
			source = s
		}
	}
	return b.String(), source
}
