package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	UntrustedStartDelimiter = "<<<UNTRUSTED_EXTERNAL_CONTENT: %s>>>\n"
	UntrustedEndDelimiter   = "\n<<<END_UNTRUSTED_EXTERNAL_CONTENT>>>"
	EscapedEndDelimiter     = "<<<ESCAPED_END_UNTRUSTED_EXTERNAL_CONTENT>>>"
)

// NeutralizeAndWrapUntrusted escapes inner occurrences of the closing tag and wraps content.
func NeutralizeAndWrapUntrusted(source, content string, maxBytes int) string {
	// Escape any inner end-delimiter to prevent prompt injection breakouts
	safeContent := strings.ReplaceAll(content, "<<<END_UNTRUSTED_EXTERNAL_CONTENT>>>", EscapedEndDelimiter)
	wrapped := fmt.Sprintf(UntrustedStartDelimiter, source) + safeContent + UntrustedEndDelimiter
	return TruncateOutput(wrapped, maxBytes)
}

type WebFetchTool struct {
	client    *http.Client
	validator *SSRFValidator
	maxOutput int
}

func NewWebFetchTool(maxOutput int) *WebFetchTool {
	validator := NewSSRFValidator()
	return &WebFetchTool{
		client:    NewSecureHTTPClient(validator, 30*time.Second),
		validator: validator,
		maxOutput: maxOutput,
	}
}

// SetSSRFValidator allows overriding the validator (useful for test harnesses).
func (t *WebFetchTool) SetSSRFValidator(v *SSRFValidator) {
	t.validator = v
	t.client = NewSecureHTTPClient(v, 30*time.Second)
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch a web page via HTTP GET and extract readable text. Output is marked as untrusted external content. Subject to SSRF validation."
}
func (t *WebFetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The URL to fetch"}
		},
		"required": ["url"]
	}`)
}

func (t *WebFetchTool) Run(argsJSON string) (string, error) {
	return t.RunWithContext(context.Background(), argsJSON)
}

func (t *WebFetchTool) RunWithContext(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("url cannot be empty")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// 1. SSRF Pre-validation
	validatedURL, err := t.validator.ValidateURL(ctx, args.URL)
	if err != nil {
		return "", fmt.Errorf("web_fetch denied by SSRF policy: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", validatedURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ZeroTwo/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", args.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP error %d from %s", resp.StatusCode, args.URL)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	plainText := StripHTML(string(body))
	return NeutralizeAndWrapUntrusted("web_fetch("+args.URL+")", plainText, t.maxOutput), nil
}

type HTTPRequestTool struct {
	client    *http.Client
	validator *SSRFValidator
	maxOutput int
}

func NewHTTPRequestTool(maxOutput int) *HTTPRequestTool {
	validator := NewSSRFValidator()
	return &HTTPRequestTool{
		client:    NewSecureHTTPClient(validator, 30*time.Second),
		validator: validator,
		maxOutput: maxOutput,
	}
}

// SetSSRFValidator allows overriding the validator (useful for test harnesses).
func (t *HTTPRequestTool) SetSSRFValidator(v *SSRFValidator) {
	t.validator = v
	t.client = NewSecureHTTPClient(v, 30*time.Second)
}

func (t *HTTPRequestTool) Name() string { return "http_request" }
func (t *HTTPRequestTool) Description() string {
	return "Make an arbitrary HTTP request (GET, POST, PUT, DELETE) with optional body and headers. Subject to SSRF validation."
}
func (t *HTTPRequestTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"method": {"type": "string", "description": "HTTP method (GET, POST, etc.)", "default": "GET"},
			"url": {"type": "string", "description": "URL to request"},
			"body": {"type": "string", "description": "Optional request body (max 64KB)"},
			"headers": {"type": "object", "description": "Optional HTTP headers"}
		},
		"required": ["url"]
	}`)
}

func (t *HTTPRequestTool) Run(argsJSON string) (string, error) {
	return t.RunWithContext(context.Background(), argsJSON)
}

func (t *HTTPRequestTool) RunWithContext(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.URL == "" {
		return "", fmt.Errorf("url cannot be empty")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// 1. SSRF Pre-validation
	validatedURL, err := t.validator.ValidateURL(ctx, args.URL)
	if err != nil {
		return "", fmt.Errorf("http_request denied by SSRF policy: %w", err)
	}

	method := strings.ToUpper(args.Method)
	if method == "" {
		method = "GET"
	}

	// 2. Bound request body size (max 64 KB)
	if len(args.Body) > 64*1024 {
		return "", fmt.Errorf("request body exceeds maximum allowed size (64 KB)")
	}

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, validatedURL.String(), bodyReader)
	if err != nil {
		return "", err
	}
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "ZeroTwo/1.0")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to %s: %w", args.URL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}

	out := fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(respBody))
	return NeutralizeAndWrapUntrusted("http_request("+args.URL+")", out, t.maxOutput), nil
}

type SearchTool struct {
	apiKey    string
	maxResult int
	client    *http.Client
	maxOutput int
}

func NewSearchTool(apiKey string, maxResult int, maxOutput int) *SearchTool {
	if maxResult <= 0 {
		maxResult = 5
	}
	return &SearchTool{
		apiKey:    apiKey,
		maxResult: maxResult,
		client:    &http.Client{Timeout: 30 * time.Second},
		maxOutput: maxOutput,
	}
}

func (t *SearchTool) Name() string { return "web_search" }
func (t *SearchTool) Description() string {
	return "Search the web via Brave Search API. Returns titles, URLs, and descriptions."
}
func (t *SearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query terms"}
		},
		"required": ["query"]
	}`)
}

func (t *SearchTool) Run(argsJSON string) (string, error) {
	return t.RunWithContext(context.Background(), argsJSON)
}

func (t *SearchTool) RunWithContext(ctx context.Context, argsJSON string) (string, error) {
	if t.apiKey == "" {
		return "", fmt.Errorf("web_search is disabled: no brave_api_key configured")
	}
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Query == "" {
		return "", fmt.Errorf("query cannot be empty")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		strings.ReplaceAll(args.Query, " ", "+"), t.maxResult)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("brave API error (status %d)", resp.StatusCode)
	}

	var braveResp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&braveResp); err != nil {
		return "", fmt.Errorf("parsing search response: %w", err)
	}

	var sb strings.Builder
	for i, r := range braveResp.Web.Results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description))
	}
	res := sb.String()
	if res == "" {
		res = "No results found."
	}

	return NeutralizeAndWrapUntrusted("web_search("+args.Query+")", res, t.maxOutput), nil
}

var (
	scriptRegex = regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
	styleRegex  = regexp.MustCompile(`(?is)<style.*?>.*?</style>`)
	tagRegex    = regexp.MustCompile(`<[^>]+>`)
	spaceRegex  = regexp.MustCompile(`\s{2,}`)
)

func StripHTML(html string) string {
	s := scriptRegex.ReplaceAllString(html, "")
	s = styleRegex.ReplaceAllString(s, "")
	s = tagRegex.ReplaceAllString(s, " ")
	s = spaceRegex.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
