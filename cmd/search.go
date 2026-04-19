package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sapihav/perplexity-cli/internal/client"
	"github.com/spf13/cobra"
)

// searchResult is one item in the command-level result.results[] array.
// Field names are part of the CLI's stable output contract (M3+): callers
// parse published_date and domain, not the raw upstream date/url fields.
type searchResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet"`
	PublishedDate string `json:"published_date"`
	Domain        string `json:"domain"`
}

// searchOutput is the payload placed under envelope.result on success.
type searchOutput struct {
	Results []searchResult `json:"results"`
}

type searchFlags struct {
	maxResults     int
	country        string
	languages      []string
	domains        []string
	excludeDomains []string
	recency        string
	dateFrom       string
	dateTo         string
}

// Perplexity's /search filters have documented caps and mutual-exclusion
// rules. Enforcing client-side gives a cleaner error than the upstream 400
// and avoids burning an API call on a request we know will be rejected.
// See https://docs.perplexity.ai/guides/search-domain-filter and
// https://docs.perplexity.ai/guides/search-language-filter.
const (
	maxSearchDomainsPerFilter = 20
	maxSearchLanguages        = 10
)

// recencyAliases maps the CLI's shorthand duration notation to the API's
// bucket names. Accepting both the shorthand (1d) and the bucket name (day)
// keeps the surface forgiving without leaking wire detail into --help.
var recencyAliases = map[string]string{
	"1h": "hour", "hour": "hour",
	"1d": "day", "day": "day",
	"1w": "week", "week": "week",
	"1m": "month", "month": "month",
	"1y": "year", "year": "year",
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func newSearchCmd() *cobra.Command {
	f := &searchFlags{}
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Ranked web results from Perplexity's /search endpoint (no AI synthesis)",
		Long: `search wraps POST /search and returns ranked web results — no AI answer.
Use "perplexity ask" for synthesized, cited answers. Pass "-" to read the
query from stdin.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args[0], f)
		},
	}
	c.Flags().IntVar(&f.maxResults, "max-results", 10, "Number of results to return (1–20)")
	c.Flags().StringVar(&f.country, "country", "", "ISO country code (e.g. US, GB) for regional results")
	c.Flags().StringArrayVar(&f.languages, "language", nil, "ISO 639-1 language code (e.g. en, fr); repeatable up to 10 entries")
	c.Flags().StringArrayVar(&f.domains, "domain", nil, "Restrict results to this domain (repeatable)")
	c.Flags().StringArrayVar(&f.excludeDomains, "exclude-domain", nil, "Exclude results from this domain (repeatable)")
	c.Flags().StringVar(&f.recency, "recency", "", "Recency window: 1h|1d|1w|1m|1y (or hour|day|week|month|year)")
	c.Flags().StringVar(&f.dateFrom, "date-from", "", "Earliest publish date (YYYY-MM-DD)")
	c.Flags().StringVar(&f.dateTo, "date-to", "", "Latest publish date (YYYY-MM-DD)")
	return c
}

func runSearch(ctx context.Context, stdout, stderr io.Writer, query string, f *searchFlags) error {
	start := time.Now()
	query, err := readStdinIfDash(query)
	if err != nil {
		errorOut(stderr, 1, err.Error())
		return err
	}

	req, err := buildSearchRequest(query, f)
	if err != nil {
		errorOut(stderr, 2, err.Error())
		return err
	}

	apiKey := requireAPIKey(stderr)
	if apiKey == "" {
		return fmt.Errorf("missing PERPLEXITY_API_KEY")
	}

	logf(stderr, "max_results=%d recency=%q country=%q languages=%v", req.MaxResults, req.RecencyFilter, req.Country, req.Language)

	c := client.New(apiKey, clientOptions()...)

	if g.dryRun {
		dump, err := c.DumpSearch(req)
		if err != nil {
			errorOut(stderr, 1, err.Error())
			return err
		}
		fmt.Fprint(stdout, dump)
		return nil
	}

	resp, err := c.Search(ctx, req)
	if err != nil {
		return handleClientError(stderr, err)
	}

	out := searchOutput{Results: mapResults(resp.Results)}
	return writeJSON(stdout, envelope{
		SchemaVersion: "1",
		Provider:      "perplexity",
		Command:       "search",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        out,
	})
}

// buildSearchRequest validates flag values and assembles the wire request.
// User input is never string-interpolated — everything flows through the JSON
// encoder on the client side.
func buildSearchRequest(query string, f *searchFlags) (client.SearchRequest, error) {
	req := client.SearchRequest{Query: query}

	if f.maxResults < 1 || f.maxResults > 20 {
		return req, fmt.Errorf("--max-results must be between 1 and 20, got %d", f.maxResults)
	}
	req.MaxResults = f.maxResults
	req.Country = strings.TrimSpace(f.country)

	langs := normalizeStringList(f.languages)
	if len(langs) > maxSearchLanguages {
		return req, fmt.Errorf("--language accepts at most %d entries (got %d)", maxSearchLanguages, len(langs))
	}
	req.Language = langs

	if f.recency != "" {
		mapped, ok := recencyAliases[strings.ToLower(f.recency)]
		if !ok {
			return req, fmt.Errorf("--recency must be one of 1h|1d|1w|1m|1y (got %q)", f.recency)
		}
		req.RecencyFilter = mapped
	}

	after, err := convertISODate(f.dateFrom, "--date-from")
	if err != nil {
		return req, err
	}
	before, err := convertISODate(f.dateTo, "--date-to")
	if err != nil {
		return req, err
	}
	// Upstream rejects --recency combined with --date-from/--date-to.
	// Catch it client-side so the error is actionable without burning a call.
	if req.RecencyFilter != "" && (after != "" || before != "") {
		return req, fmt.Errorf("--recency cannot be combined with --date-from / --date-to; pick one")
	}
	req.SearchAfter = after
	req.SearchBefore = before

	includes := normalizeStringList(f.domains)
	excludes := normalizeStringList(f.excludeDomains)
	if len(includes) > 0 && len(excludes) > 0 {
		return req, fmt.Errorf("--domain (allowlist) and --exclude-domain (denylist) are mutually exclusive per the Perplexity API; use only one")
	}
	if len(includes) > maxSearchDomainsPerFilter {
		return req, fmt.Errorf("--domain accepts at most %d entries (got %d)", maxSearchDomainsPerFilter, len(includes))
	}
	if len(excludes) > maxSearchDomainsPerFilter {
		return req, fmt.Errorf("--exclude-domain accepts at most %d entries (got %d)", maxSearchDomainsPerFilter, len(excludes))
	}
	req.DomainFilter = buildDomainFilter(includes, excludes)
	return req, nil
}

// normalizeStringList trims whitespace, drops empties, and returns nil when
// no real entries remain so omitempty keeps them off the wire.
func normalizeStringList(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// convertISODate validates YYYY-MM-DD input and converts to the API's
// MM/DD/YYYY wire format. Empty input passes through as empty.
func convertISODate(s, flag string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !isoDate.MatchString(s) {
		return "", fmt.Errorf("%s must be YYYY-MM-DD (got %q)", flag, s)
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", fmt.Errorf("%s invalid date: %w", flag, err)
	}
	return t.Format("01/02/2006"), nil
}

// buildDomainFilter turns pre-validated include OR exclude lists (never both
// non-empty — the caller enforces mutual exclusion per the Perplexity API)
// into the wire array. Exclude entries get the "-" prefix per convention.
func buildDomainFilter(include, exclude []string) []string {
	var out []string
	for _, d := range include {
		if d != "" {
			out = append(out, d)
		}
	}
	for _, d := range exclude {
		if d != "" {
			out = append(out, "-"+d)
		}
	}
	return out
}

// mapResults adapts the upstream wire shape (date, url) to the CLI's stable
// output shape (published_date, domain). Domain is derived via net/url so we
// don't crack URLs by hand.
func mapResults(in []client.SearchResult) []searchResult {
	out := make([]searchResult, 0, len(in))
	for _, r := range in {
		out = append(out, searchResult{
			Title:         r.Title,
			URL:           r.URL,
			Snippet:       r.Snippet,
			PublishedDate: r.Date,
			Domain:        domainFromURL(r.URL),
		})
	}
	return out
}

func domainFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	// Strip a leading "www." so callers can group results by site without
	// having to normalize themselves.
	return strings.TrimPrefix(u.Hostname(), "www.")
}
