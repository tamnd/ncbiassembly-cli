package ncbiassembly

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network. The client's HTTP behaviour is
// covered in ncbiassembly_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "ncbiassembly" {
		t.Errorf("Scheme = %q, want ncbiassembly", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "ncbiassembly" {
		t.Errorf("Identity.Binary = %q, want ncbiassembly", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"33196251", "assembly", "33196251"},
		{"GCA_057084165.1", "assembly", "GCA_057084165.1"},
		{"Homo sapiens", "assembly", "Homo sapiens"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return an error")
	}
	_, _, err = Domain{}.Classify("   ")
	if err == nil {
		t.Error("Classify(whitespace) should return an error")
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType, id, want string
	}{
		{"assembly", "33196251", "https://www.ncbi.nlm.nih.gov/assembly/33196251"},
		{"assembly", "GCA_057084165.1", "https://www.ncbi.nlm.nih.gov/assembly/GCA_057084165.1"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("Locate(%q, %q) = (%q, %v), want (%q, nil)",
				tc.uriType, tc.id, got, err, tc.want)
		}
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("page", "foo")
	if err == nil {
		t.Error("Locate with unknown type should return an error")
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	a := &Assembly{
		ID:        "33196251",
		Accession: "GCA_057084165.1",
		Organism:  "Homo sapiens (human)",
	}
	u, err := h.Mint(a)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "ncbiassembly://assembly/33196251"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("ncbiassembly", "12345678")
	if err != nil || got.String() != "ncbiassembly://assembly/12345678" {
		t.Errorf("ResolveOn = (%q, %v), want ncbiassembly://assembly/12345678", got.String(), err)
	}
}
