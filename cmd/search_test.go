package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
)

// genStringList returns N distinct non-empty strings — used to exercise the
// client-side count caps on --language / --domain / --exclude-domain.
func genStringList(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("s%d.example", i)
	}
	return out
}

// mockSearchServer is an httptest.Server that captures the first POST body
// it receives so tests can assert the exact wire contract the CLI emits.
type capturedRequest struct {
	path        string
	method      string
	contentType string
	authHeader  string
	body        map[string]any
}

func mockSearchServer(t *testing.T, status int, resp string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		cap.method = r.Method
		cap.contentType = r.Header.Get("Content-Type")
		cap.authHeader = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &cap.body)
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(resp))
	}))
	return srv, cap
}

// TestRunSearch_HappyPath_WritesEnvelope asserts the full happy-path: the
// wire call targets /search, request carries the mapped flags, and the
// stdout envelope follows the new result.results[] contract (no answer).
func TestRunSearch_HappyPath_WritesEnvelope(t *testing.T) {
	resetGlobals(t)
	respBody := `{"results":[{"title":"Paris","url":"https://en.wikipedia.org/wiki/Paris","snippet":"Capital of France","date":"2024-01-15","last_updated":"2024-02-01"}]}`
	srv, cap := mockSearchServer(t, 0, respBody)
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{
		client.WithSearchEndpoint(srv.URL),
		client.WithBackoff(func(int) time.Duration { return 0 }),
	}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	// Scenario A: allowlist domains + recency (mutually compatible).
	f := &searchFlags{
		maxResults: 5,
		country:    "US",
		languages:  []string{"en", "fr"},
		domains:    []string{"wikipedia.org"},
		recency:    "1m",
	}
	if err := runSearch(context.Background(), &stdout, &stderr, "capital of france?", f); err != nil {
		t.Fatalf("runSearch: %v\n%s", err, stderr.String())
	}

	// Golden request shape: assert the endpoint, method, headers and every
	// JSON field the CLI builds. No string interpolation bugs allowed.
	if cap.method != "POST" {
		t.Errorf("method = %q, want POST", cap.method)
	}
	if cap.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", cap.contentType)
	}
	if cap.authHeader != "Bearer k" {
		t.Errorf("Authorization = %q, want Bearer k", cap.authHeader)
	}
	if cap.body["query"] != "capital of france?" {
		t.Errorf("query = %v", cap.body["query"])
	}
	if cap.body["max_results"].(float64) != 5 {
		t.Errorf("max_results = %v, want 5", cap.body["max_results"])
	}
	if cap.body["country"] != "US" {
		t.Errorf("country = %v", cap.body["country"])
	}
	// Language must be an ARRAY of ISO codes per the Perplexity API, not a
	// string. Assert the wire shape explicitly.
	langs, ok := cap.body["search_language_filter"].([]any)
	if !ok || len(langs) != 2 || langs[0] != "en" || langs[1] != "fr" {
		t.Errorf("search_language_filter must be array [\"en\",\"fr\"]; got %T %v", cap.body["search_language_filter"], cap.body["search_language_filter"])
	}
	if cap.body["search_recency_filter"] != "month" {
		t.Errorf("recency = %v, want month", cap.body["search_recency_filter"])
	}
	if _, hasAfter := cap.body["search_after_date"]; hasAfter {
		t.Errorf("search_after_date must be absent when --date-from not set")
	}
	if _, hasBefore := cap.body["search_before_date"]; hasBefore {
		t.Errorf("search_before_date must be absent when --date-to not set")
	}
	doms, _ := cap.body["search_domain_filter"].([]any)
	if len(doms) != 1 || doms[0] != "wikipedia.org" {
		t.Errorf("domain filter = %v", doms)
	}

	// Envelope + result shape.
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout.String())
	}
	if env["command"] != "search" || env["schema_version"] != "1" {
		t.Errorf("envelope mismatch: %+v", env)
	}
	if _, hasAnswer := env["result"].(map[string]any)["answer"]; hasAnswer {
		t.Error("result must not carry 'answer' on the /search endpoint")
	}
	results := env["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	r0 := results[0].(map[string]any)
	if r0["title"] != "Paris" || r0["url"] != "https://en.wikipedia.org/wiki/Paris" ||
		r0["snippet"] != "Capital of France" || r0["published_date"] != "2024-01-15" ||
		r0["domain"] != "en.wikipedia.org" {
		t.Errorf("result row mismatch: %+v", r0)
	}
}

func TestRunSearch_DryRun_RedactsAuthAndSkipsNetwork(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "pplx-super-secret")
	g.dryRun = true

	var stdout, stderr bytes.Buffer
	f := &searchFlags{maxResults: 10}
	if err := runSearch(context.Background(), &stdout, &stderr, "hello", f); err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	s := stdout.String()
	if !strings.Contains(s, "POST https://api.perplexity.ai/search") {
		t.Errorf("dry-run missing /search endpoint line: %q", s)
	}
	if !strings.Contains(s, "Authorization: Bearer ***REDACTED***") {
		t.Errorf("dry-run did not redact Authorization: %q", s)
	}
	if strings.Contains(s, "pplx-super-secret") {
		t.Fatalf("dry-run leaked the API key: %q", s)
	}
	if !strings.Contains(s, `"query": "hello"`) {
		t.Errorf("dry-run missing query: %q", s)
	}
}

func TestRunSearch_MissingAPIKey_ExitsCode2(t *testing.T) {
	resetGlobals(t)
	t.Setenv("PERPLEXITY_API_KEY", "")
	var stdout, stderr bytes.Buffer
	f := &searchFlags{maxResults: 10}
	err := runSearch(context.Background(), &stdout, &stderr, "hello", f)
	if err == nil {
		t.Fatal("expected error when PERPLEXITY_API_KEY is unset")
	}
	if ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", ExitCode())
	}
	if !strings.Contains(stderr.String(), "PERPLEXITY_API_KEY") {
		t.Errorf("stderr missing env var name: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on config error, got %q", stdout.String())
	}
}

func TestRunSearch_APIError_ExitCode1(t *testing.T) {
	resetGlobals(t)
	srv, _ := mockSearchServer(t, http.StatusBadRequest, `{"error":"bad query"}`)
	defer srv.Close()

	t.Setenv("PERPLEXITY_API_KEY", "k")
	extraClientOptions = []client.Option{
		client.WithSearchEndpoint(srv.URL),
		client.WithBackoff(func(int) time.Duration { return 0 }),
	}
	defer func() { extraClientOptions = nil }()

	var stdout, stderr bytes.Buffer
	f := &searchFlags{maxResults: 10}
	err := runSearch(context.Background(), &stdout, &stderr, "q", f)
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCode() != 1 {
		t.Errorf("exit = %d, want 1", ExitCode())
	}
	if !strings.Contains(stderr.String(), "400") {
		t.Errorf("stderr missing 400: %q", stderr.String())
	}
}

func TestBuildSearchRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		flags searchFlags
		want  string
	}{
		{"max_results too low", searchFlags{maxResults: 0}, "--max-results"},
		{"max_results too high", searchFlags{maxResults: 21}, "--max-results"},
		{"bad recency", searchFlags{maxResults: 10, recency: "2 weeks"}, "--recency"},
		{"bad date-from", searchFlags{maxResults: 10, dateFrom: "2024/01/01"}, "--date-from"},
		{"bad date-to", searchFlags{maxResults: 10, dateTo: "01-01-2024"}, "--date-to"},
		{"domain allow+deny mutually exclusive",
			searchFlags{maxResults: 10, domains: []string{"a.com"}, excludeDomains: []string{"b.com"}},
			"mutually exclusive",
		},
		{"recency + date-from forbidden",
			searchFlags{maxResults: 10, recency: "1w", dateFrom: "2024-01-01"},
			"--recency cannot be combined",
		},
		{"recency + date-to forbidden",
			searchFlags{maxResults: 10, recency: "1w", dateTo: "2024-01-01"},
			"--recency cannot be combined",
		},
		{"too many languages",
			searchFlags{maxResults: 10, languages: genStringList(maxSearchLanguages + 1)},
			"--language accepts at most",
		},
		{"too many domains",
			searchFlags{maxResults: 10, domains: genStringList(maxSearchDomainsPerFilter + 1)},
			"--domain accepts at most",
		},
		{"too many exclude-domains",
			searchFlags{maxResults: 10, excludeDomains: genStringList(maxSearchDomainsPerFilter + 1)},
			"--exclude-domain accepts at most",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildSearchRequest("q", &tc.flags)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got err=%v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestBuildSearchRequest_OmitsEmptyOptionalFields(t *testing.T) {
	req, err := buildSearchRequest("q", &searchFlags{maxResults: 10})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	b, _ := json.Marshal(req)
	// With only max_results set, the on-wire JSON should be minimal. omitempty
	// on optional wire fields keeps the API from rejecting empty values.
	s := string(b)
	for _, banned := range []string{"country", "search_language_filter", "search_recency_filter", "search_after_date", "search_before_date", "search_domain_filter"} {
		if strings.Contains(s, banned) {
			t.Errorf("expected %q absent from wire body, got %s", banned, s)
		}
	}
}

func TestBuildSearchRequest_RecencyAliasMapping(t *testing.T) {
	cases := map[string]string{"1h": "hour", "1d": "day", "1w": "week", "1m": "month", "1y": "year", "DAY": "day", "Week": "week"}
	for in, want := range cases {
		req, err := buildSearchRequest("q", &searchFlags{maxResults: 10, recency: in})
		if err != nil {
			t.Errorf("recency=%q: %v", in, err)
			continue
		}
		if req.RecencyFilter != want {
			t.Errorf("recency=%q mapped to %q, want %q", in, req.RecencyFilter, want)
		}
	}
}

func TestBuildDomainFilter_NegativePrefix(t *testing.T) {
	// buildDomainFilter expects pre-normalized slices and will NEVER see
	// both include and exclude non-empty (the caller enforces mutual
	// exclusion per the Perplexity API). Exercise each mode independently.
	if got := buildDomainFilter([]string{"a.com"}, nil); len(got) != 1 || got[0] != "a.com" {
		t.Errorf("allowlist: %v", got)
	}
	if got := buildDomainFilter(nil, []string{"b.com"}); len(got) != 1 || got[0] != "-b.com" {
		t.Errorf("denylist: %v", got)
	}
	if buildDomainFilter(nil, nil) != nil {
		t.Error("empty inputs should produce nil slice")
	}
}

// TestBuildSearchRequest_DenylistDomainsAndDates exercises the alternative
// valid-combination path: exclude-only domains + --date-from/--date-to,
// which complements the allowlist+recency scenario in the main golden test.
func TestBuildSearchRequest_DenylistDomainsAndDates(t *testing.T) {
	req, err := buildSearchRequest("q", &searchFlags{
		maxResults:     10,
		excludeDomains: []string{"spam.example", "ads.example"},
		dateFrom:       "2024-01-01",
		dateTo:         "2024-12-31",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(req.DomainFilter) != 2 || req.DomainFilter[0] != "-spam.example" || req.DomainFilter[1] != "-ads.example" {
		t.Errorf("domain filter = %v", req.DomainFilter)
	}
	if req.SearchAfter != "01/01/2024" || req.SearchBefore != "12/31/2024" {
		t.Errorf("dates = %q / %q", req.SearchAfter, req.SearchBefore)
	}
	if req.RecencyFilter != "" {
		t.Errorf("recency must stay empty when dates are set; got %q", req.RecencyFilter)
	}
}

func TestDomainFromURL_StripsWWW(t *testing.T) {
	cases := map[string]string{
		"https://www.example.com/path":   "example.com",
		"https://sub.example.com/":       "sub.example.com",
		"":                               "",
		"not a url":                      "",
		"http://example.com":             "example.com",
	}
	for in, want := range cases {
		if got := domainFromURL(in); got != want {
			t.Errorf("domainFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWriteJSON_EnvelopeShape asserts the stable stdout envelope on the new
// searchOutput payload: schema_version, provider, command, elapsed_ms,
// result.results[]. This is the contract downstream agents depend on.
func TestWriteJSON_EnvelopeShape(t *testing.T) {
	resetGlobals(t)
	env := envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "search",
		ElapsedMs:     42,
		Result: searchOutput{Results: []searchResult{{
			Title: "Paris", URL: "https://example.com", Snippet: "s", PublishedDate: "2024-01-15", Domain: "example.com",
		}}},
	}
	var buf bytes.Buffer
	if err := writeJSON(&buf, env); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"schema_version", "provider", "command", "elapsed_ms", "result"} {
		if _, ok := got[key]; !ok {
			t.Errorf("envelope missing key %q; got %v", key, got)
		}
	}
	results := got["result"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	r0 := results[0].(map[string]any)
	if r0["published_date"] != "2024-01-15" || r0["domain"] != "example.com" {
		t.Errorf("result row mismatch: %+v", r0)
	}
}
