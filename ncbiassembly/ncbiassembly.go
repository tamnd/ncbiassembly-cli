// Package ncbiassembly is the library behind the ncbiassembly command line:
// the HTTP client, request shaping, and the typed data models for the NCBI
// Assembly database (3.6M+ genome assemblies).
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
// Search and summary calls follow the standard eUtils two-step:
// esearch returns a list of numeric UIDs, esummary turns them into records.
package ncbiassembly

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to NCBI.
const DefaultUserAgent = "ncbiassembly-cli/dev (+https://github.com/tamnd/ncbiassembly-cli)"

// Host is the NCBI Assembly web host, used for Locate / URI resolution.
const Host = "www.ncbi.nlm.nih.gov"

// baseURL is the root every eUtils request is built from.
const baseURL = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

// email and tool are appended to every eUtils request as required by NCBI policy.
const (
	ncbiEmail = "tamnd87@gmail.com"
	ncbiTool  = "ncbiassembly-cli"
)

// Config carries the tunable parameters for the Assembly client.
type Config struct {
	BaseURL   string
	UserAgent string
	Rate      time.Duration
	Timeout   time.Duration
	Retries   int
}

// DefaultConfig returns a Config with sensible defaults for the free-tier
// NCBI eUtils API (3 req/s without a key).
func DefaultConfig() Config {
	return Config{
		BaseURL:   baseURL,
		UserAgent: DefaultUserAgent,
		Rate:      400 * time.Millisecond,
		Timeout:   30 * time.Second,
		Retries:   3,
	}
}

// Client talks to the NCBI eUtils Assembly API over HTTP.
type Client struct {
	HTTP *http.Client
	cfg  Config
	last time.Time
	// exported shims so domain.go newClient can set them like the scaffold does
	UserAgent string
	Rate      time.Duration
	Retries   int
}

// NewClient returns a Client with the default config.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		cfg:       cfg,
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// NewClientWithConfig returns a Client using the given config.
func NewClientWithConfig(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = baseURL
	}
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		cfg:       cfg,
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Get fetches rawURL and returns the response body, pacing and retrying.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	retries := c.cfg.Retries
	if retries <= 0 {
		retries = c.Retries
	}
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	ua := c.cfg.UserAgent
	if ua == "" {
		ua = c.UserAgent
	}
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	rate := c.cfg.Rate
	if rate <= 0 {
		rate = c.Rate
	}
	if rate <= 0 {
		return
	}
	if wait := rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// buildURL constructs an eUtils endpoint URL with the given parameters,
// always appending the required email and tool fields.
func (c *Client) buildURL(endpoint string, params url.Values) string {
	params.Set("retmode", "json")
	params.Set("email", ncbiEmail)
	params.Set("tool", ncbiTool)
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	return base + "/" + endpoint + "?" + params.Encode()
}

// --- wire types (unexported) ---

type wireSearch struct {
	ESearchResult struct {
		Count  string   `json:"count"`
		IDList []string `json:"idlist"`
	} `json:"esearchresult"`
}

type wireAssembly struct {
	UID            string `json:"uid"`
	Accession      string `json:"assemblyaccession"`
	Name           string `json:"assemblyname"`
	TaxID          string `json:"taxid"`
	Organism       string `json:"organism"`
	SpeciesName    string `json:"speciesname"`
	AssemblyType   string `json:"assemblytype"`
	AssemblyStatus string `json:"assemblystatus"`
	WGS            string `json:"wgs"`
	BioSampleAccn  string `json:"biosampleaccn"`
}

type wireSummary struct {
	Result map[string]json.RawMessage `json:"result"`
}

// --- public output types ---

// Assembly is a single NCBI Assembly record.
type Assembly struct {
	ID          string `json:"id"             kit:"id"` // numeric UID
	Accession   string `json:"accession,omitempty"`
	Name        string `json:"name,omitempty"`
	Organism    string `json:"organism,omitempty"`
	SpeciesName string `json:"species,omitempty"`
	TaxID       string `json:"tax_id,omitempty"`
	Type        string `json:"type,omitempty"`
	Status      string `json:"status,omitempty"`
	WGS         string `json:"wgs,omitempty"`
	BioSample   string `json:"biosample,omitempty"`
}

// --- client methods ---

// Search runs an esearch query against the Assembly database and returns
// a list of numeric UIDs plus the total count.
func (c *Client) Search(ctx context.Context, query string, limit, start int) ([]string, int, error) {
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{
		"db":       {"assembly"},
		"term":     {query},
		"retmax":   {strconv.Itoa(limit)},
		"retstart": {strconv.Itoa(start)},
	}
	body, err := c.Get(ctx, c.buildURL("esearch.fcgi", params))
	if err != nil {
		return nil, 0, err
	}
	var ws wireSearch
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, 0, fmt.Errorf("esearch parse: %w", err)
	}
	count, _ := strconv.Atoi(ws.ESearchResult.Count)
	ids := ws.ESearchResult.IDList
	if ids == nil {
		ids = []string{}
	}
	return ids, count, nil
}

// FetchAssemblies fetches esummary for a batch of Assembly UIDs and returns Assembly records.
func (c *Client) FetchAssemblies(ctx context.Context, ids []string) ([]*Assembly, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	params := url.Values{
		"db": {"assembly"},
		"id": {strings.Join(ids, ",")},
	}
	body, err := c.Get(ctx, c.buildURL("esummary.fcgi", params))
	if err != nil {
		return nil, err
	}
	var ws wireSummary
	if err := json.Unmarshal(body, &ws); err != nil {
		return nil, fmt.Errorf("esummary parse: %w", err)
	}
	uids, err := parseUIDs(ws.Result)
	if err != nil {
		return nil, err
	}
	var out []*Assembly
	for _, uid := range uids {
		raw, ok := ws.Result[uid]
		if !ok {
			continue
		}
		var wa wireAssembly
		if err := json.Unmarshal(raw, &wa); err != nil {
			continue
		}
		out = append(out, assemblyFromWire(uid, wa))
	}
	return out, nil
}

// GetAssembly fetches a single Assembly record by numeric UID.
func (c *Client) GetAssembly(ctx context.Context, uid string) (*Assembly, error) {
	assemblies, err := c.FetchAssemblies(ctx, []string{uid})
	if err != nil {
		return nil, err
	}
	if len(assemblies) == 0 {
		return nil, fmt.Errorf("assembly uid %s: not found", uid)
	}
	return assemblies[0], nil
}

// SearchAndFetch runs Search then FetchAssemblies in one call.
func (c *Client) SearchAndFetch(ctx context.Context, query string, limit, start int) ([]*Assembly, int, error) {
	ids, count, err := c.Search(ctx, query, limit, start)
	if err != nil {
		return nil, 0, err
	}
	if len(ids) == 0 {
		return nil, count, nil
	}
	assemblies, err := c.FetchAssemblies(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	return assemblies, count, nil
}

// --- helpers ---

// parseUIDs extracts the ordered UID list from the esummary result map.
func parseUIDs(result map[string]json.RawMessage) ([]string, error) {
	raw, ok := result["uids"]
	if !ok {
		return nil, nil
	}
	var uids []string
	if err := json.Unmarshal(raw, &uids); err != nil {
		return nil, fmt.Errorf("parse uids: %w", err)
	}
	return uids, nil
}

// assemblyFromWire converts the wire representation to an Assembly.
func assemblyFromWire(uid string, wa wireAssembly) *Assembly {
	id := wa.UID
	if id == "" {
		id = uid
	}
	return &Assembly{
		ID:          id,
		Accession:   wa.Accession,
		Name:        wa.Name,
		Organism:    wa.Organism,
		SpeciesName: wa.SpeciesName,
		TaxID:       wa.TaxID,
		Type:        wa.AssemblyType,
		Status:      wa.AssemblyStatus,
		WGS:         wa.WGS,
		BioSample:   wa.BioSampleAccn,
	}
}
