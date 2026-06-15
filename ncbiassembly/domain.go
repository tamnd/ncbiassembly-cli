package ncbiassembly

import (
	"context"
	"strings"
	"unicode"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes NCBI Assembly as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/ncbiassembly-cli/ncbiassembly"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// ncbiassembly:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone ncbiassembly binary (see cmd/ncbiassembly/main.go),
// so the binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the NCBI Assembly driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "ncbiassembly",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "ncbiassembly",
			Short:  "Read public NCBI Assembly genome assembly records.",
			Long: `Read public NCBI Assembly genome assembly records.

ncbiassembly reads from the NCBI Assembly database (3.6M+ genome assemblies)
over plain HTTPS, shapes it into clean records, and prints output that pipes
into the rest of your tools. No API key required.`,
			Site: "www.ncbi.nlm.nih.gov/assembly",
			Repo: "https://github.com/tamnd/ncbiassembly-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: full-text search across the Assembly database, returns Assembly records.
	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search NCBI Assembly and return assembly records",
		Args:    []kit.Arg{{Name: "query", Help: "search terms", Variadic: true}}}, searchAssemblies)

	// assembly: fetch a single Assembly record by numeric UID.
	kit.Handle(app, kit.OpMeta{Name: "assembly", Group: "read", Single: true,
		Summary: "Fetch an Assembly record by numeric UID", URIType: "assembly", Resolver: true,
		Args: []kit.Arg{{Name: "uid", Help: "Assembly numeric UID"}}}, getAssembly)

	// organism: search Assembly records by organism name.
	kit.Handle(app, kit.OpMeta{Name: "organism", Group: "read", List: true,
		Summary: "Search Assembly records by organism name",
		Args:    []kit.Arg{{Name: "name", Help: "organism name", Variadic: true}}}, searchOrganism)
}

// newClient builds the Assembly client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	acfg := DefaultConfig()
	if cfg.UserAgent != "" {
		acfg.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		acfg.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		acfg.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		acfg.Timeout = cfg.Timeout
	}
	return NewClientWithConfig(acfg), nil
}

// --- inputs ---

type searchInput struct {
	Query  []string `kit:"arg,variadic" help:"search terms"`
	Limit  int      `kit:"flag,inherit" help:"max results"`
	Start  int      `kit:"flag" help:"result offset"`
	Client *Client  `kit:"inject"`
}

type assemblyRef struct {
	UID    string  `kit:"arg" help:"Assembly numeric UID"`
	Client *Client `kit:"inject"`
}

type organismInput struct {
	Name   []string `kit:"arg,variadic" help:"organism name"`
	Limit  int      `kit:"flag,inherit" help:"max results"`
	Start  int      `kit:"flag" help:"result offset"`
	Client *Client  `kit:"inject"`
}

// --- handlers ---

func searchAssemblies(ctx context.Context, in searchInput, emit func(*Assembly) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	assemblies, _, err := in.Client.SearchAndFetch(ctx, strings.Join(in.Query, " "), limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, a := range assemblies {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func getAssembly(ctx context.Context, in assemblyRef, emit func(*Assembly) error) error {
	uid := in.UID
	if !isDigits(uid) {
		return errs.Usage("assembly uid must be numeric, got %q", uid)
	}
	a, err := in.Client.GetAssembly(ctx, uid)
	if err != nil {
		return mapErr(err)
	}
	return emit(a)
}

func searchOrganism(ctx context.Context, in organismInput, emit func(*Assembly) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	name := strings.Join(in.Name, " ")
	q := name + "[orgn]"
	assemblies, _, err := in.Client.SearchAndFetch(ctx, q, limit, in.Start)
	if err != nil {
		return mapErr(err)
	}
	for _, a := range assemblies {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: pure string functions, no network ---

// Classify turns any accepted input into the canonical (type, id).
// Non-empty strings are accepted and returned as assembly IDs.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty assembly reference")
	}
	return "assembly", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "assembly" {
		return "", errs.Usage("ncbiassembly has no resource type %q", uriType)
	}
	return "https://www.ncbi.nlm.nih.gov/assembly/" + id, nil
}

// --- helpers ---

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return err
}
