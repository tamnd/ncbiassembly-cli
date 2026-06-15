package ncbiassembly

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockSearchBody returns a minimal esearch JSON response with the given UIDs.
func mockSearchBody(count int, ids []string) []byte {
	b, _ := json.Marshal(map[string]any{
		"esearchresult": map[string]any{
			"count":  fmt.Sprintf("%d", count),
			"idlist": ids,
		},
	})
	return b
}

// mockSummaryBody returns a minimal esummary JSON response for the given UID.
func mockSummaryBody(uid string) []byte {
	rec := map[string]any{
		"uid":              uid,
		"assemblyaccession": "GCA_057084165.1",
		"assemblyname":     "HG03453_haplotype2_hprc_rel2_verkko2_supp",
		"taxid":            "9606",
		"organism":         "Homo sapiens (human)",
		"speciesname":      "Homo sapiens",
		"assemblytype":     "haploid (haplotype 2)",
		"assemblystatus":   "Chromosome",
		"wgs":              "JBVUML01",
		"biosampleaccn":    "SAMN17861668",
	}
	result := map[string]any{
		"uids": []string{uid},
		uid:    rec,
	}
	b, _ := json.Marshal(map[string]any{"result": result})
	return b
}

func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 0
	cfg.Timeout = 5 * time.Second
	return NewClientWithConfig(cfg)
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5
	c.cfg.Retries = 5
	c.cfg.Rate = 0

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/esearch.fcgi" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if db := r.URL.Query().Get("db"); db != "assembly" {
			t.Errorf("db = %q, want assembly", db)
		}
		if email := r.URL.Query().Get("email"); email != ncbiEmail {
			t.Errorf("email = %q, want %q", email, ncbiEmail)
		}
		if tool := r.URL.Query().Get("tool"); tool != ncbiTool {
			t.Errorf("tool = %q, want %q", tool, ncbiTool)
		}
		_, _ = w.Write(mockSearchBody(3673573, []string{"33196251", "33196250", "33196249"}))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	ids, count, err := c.Search(context.Background(), "Homo sapiens", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3673573 {
		t.Errorf("count = %d, want 3673573", count)
	}
	if len(ids) != 3 {
		t.Errorf("len(ids) = %d, want 3", len(ids))
	}
	if ids[0] != "33196251" {
		t.Errorf("ids[0] = %q, want 33196251", ids[0])
	}
}

func TestFetchAssemblies(t *testing.T) {
	const uid = "33196251"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/esummary.fcgi" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if db := r.URL.Query().Get("db"); db != "assembly" {
			t.Errorf("db = %q, want assembly", db)
		}
		_, _ = w.Write(mockSummaryBody(uid))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	assemblies, err := c.FetchAssemblies(context.Background(), []string{uid})
	if err != nil {
		t.Fatal(err)
	}
	if len(assemblies) != 1 {
		t.Fatalf("len(assemblies) = %d, want 1", len(assemblies))
	}
	a := assemblies[0]
	if a.ID != uid {
		t.Errorf("ID = %q, want %q", a.ID, uid)
	}
	if a.Accession != "GCA_057084165.1" {
		t.Errorf("Accession = %q, want GCA_057084165.1", a.Accession)
	}
	if a.Name != "HG03453_haplotype2_hprc_rel2_verkko2_supp" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.Organism != "Homo sapiens (human)" {
		t.Errorf("Organism = %q, want Homo sapiens (human)", a.Organism)
	}
	if a.SpeciesName != "Homo sapiens" {
		t.Errorf("SpeciesName = %q, want Homo sapiens", a.SpeciesName)
	}
	if a.TaxID != "9606" {
		t.Errorf("TaxID = %q, want 9606", a.TaxID)
	}
	if a.Type != "haploid (haplotype 2)" {
		t.Errorf("Type = %q, want haploid (haplotype 2)", a.Type)
	}
	if a.Status != "Chromosome" {
		t.Errorf("Status = %q, want Chromosome", a.Status)
	}
	if a.WGS != "JBVUML01" {
		t.Errorf("WGS = %q, want JBVUML01", a.WGS)
	}
	if a.BioSample != "SAMN17861668" {
		t.Errorf("BioSample = %q, want SAMN17861668", a.BioSample)
	}
}

func TestGetAssembly(t *testing.T) {
	const uid = "33196251"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mockSummaryBody(uid))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	a, err := c.GetAssembly(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != uid {
		t.Errorf("ID = %q, want %q", a.ID, uid)
	}
}

func TestSearchAndFetch(t *testing.T) {
	const uid = "33196251"
	var reqCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		switch r.URL.Path {
		case "/esearch.fcgi":
			_, _ = w.Write(mockSearchBody(1, []string{uid}))
		case "/esummary.fcgi":
			_, _ = w.Write(mockSummaryBody(uid))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	assemblies, count, err := c.SearchAndFetch(context.Background(), "Homo sapiens", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	if len(assemblies) != 1 {
		t.Fatalf("len(assemblies) = %d, want 1", len(assemblies))
	}
	if assemblies[0].ID != uid {
		t.Errorf("ID = %q, want %q", assemblies[0].ID, uid)
	}
	if reqCount != 2 {
		t.Errorf("server requests = %d, want 2 (search + summary)", reqCount)
	}
}
