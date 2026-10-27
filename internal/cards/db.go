// Package cards holds the locally cached card pool and lookup/search helpers.
package cards

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"deckbuilder/internal/riftcodex"
)

type Card = riftcodex.Card

var (
	// ErrNoCache is returned by Load when the cache file has not been created yet.
	ErrNoCache = errors.New("card cache not found; run `sync` first")
	// ErrNotFound and ErrAmbiguous are wrapped by Resolve.
	ErrNotFound  = errors.New("card not found")
	ErrAmbiguous = errors.New("ambiguous card reference")
)

type cacheFile struct {
	SyncedAt time.Time `json:"synced_at"`
	Cards    []Card    `json:"cards"`
}

type DB struct {
	SyncedAt time.Time
	Cards    []Card
	byID     map[string]*Card
}

func New(cards []Card, syncedAt time.Time) *DB {
	db := &DB{Cards: cards, SyncedAt: syncedAt, byID: make(map[string]*Card, len(cards))}
	for i := range db.Cards {
		db.byID[strings.ToLower(db.Cards[i].RiftboundID)] = &db.Cards[i]
	}
	return db
}

func Load(path string) (*DB, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoCache
	}
	if err != nil {
		return nil, err
	}
	db, err := Decode(b)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return db, nil
}

func Save(path string, cards []Card) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := Encode(cards)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Encode serializes cards in the cache format, stamped with the current time.
// The same format is used for the local cache and the copy stored in S3.
func Encode(cards []Card) ([]byte, error) {
	return json.Marshal(cacheFile{SyncedAt: time.Now().UTC(), Cards: cards})
}

func Decode(b []byte) (*DB, error) {
	var f cacheFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return New(f.Cards, f.SyncedAt), nil
}

// ByID returns the card with the given Riftbound ID (e.g. "unl-121-219").
func (db *DB) ByID(id string) (*Card, bool) {
	c, ok := db.byID[strings.ToLower(id)]
	return c, ok
}

// Resolve finds a single card from a user-supplied reference: a Riftbound ID,
// an exact name, or an unambiguous name substring. When several printings
// share a name, the standard (non-variant) printing is preferred.
func (db *DB) Resolve(ref string) (*Card, error) {
	ref = strings.TrimSpace(ref)
	if c, ok := db.ByID(ref); ok {
		return c, nil
	}
	q := strings.ToLower(ref)

	var exact, partial []*Card
	for i := range db.Cards {
		c := &db.Cards[i]
		name := strings.ToLower(c.Name)
		switch {
		case name == q || strings.ToLower(BaseName(c)) == q:
			exact = append(exact, c)
		case strings.Contains(name, q):
			partial = append(partial, c)
		}
	}
	if c := pickPrinting(exact); c != nil {
		return c, nil
	}
	if c := pickPrinting(partial); c != nil {
		return c, nil
	}
	if len(exact)+len(partial) == 0 {
		return nil, fmt.Errorf("%w: nothing matches %q", ErrNotFound, ref)
	}
	candidates := append(exact, partial...)
	var names []string
	seen := map[string]bool{}
	for _, c := range candidates {
		if n := BaseName(c); !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	slices.Sort(names)
	if len(names) > 10 {
		names = append(names[:10], "...")
	}
	return nil, fmt.Errorf("%w: %q matches %s", ErrAmbiguous, ref, strings.Join(names, ", "))
}

// pickPrinting returns a card if all candidates are printings of the same base
// name, preferring the standard printing.
func pickPrinting(cs []*Card) *Card {
	if len(cs) == 0 {
		return nil
	}
	base := BaseName(cs[0])
	for _, c := range cs[1:] {
		if BaseName(c) != base {
			return nil
		}
	}
	return slices.MinFunc(cs, cmpPrinting)
}

// printingRank orders printings of the same card: standard printings first,
// then promo/showcase reprints, then named variants like "(Alternate Art)".
func printingRank(c *Card) int {
	switch {
	case IsVariant(c):
		return 2
	case c.Classification.Rarity == "Promo" || c.Classification.Rarity == "Showcase":
		return 1
	}
	return 0
}

func cmpPrinting(a, b *Card) int {
	if d := printingRank(a) - printingRank(b); d != 0 {
		return d
	}
	return strings.Compare(a.RiftboundID, b.RiftboundID)
}

var variantSuffix = regexp.MustCompile(`\s*\([^()]*\)$`)

// BaseName strips printing suffixes like "(Alternate Art)" or "(Overnumbered)",
// so that different printings of a card compare equal for copy limits.
func BaseName(c *Card) string {
	return variantSuffix.ReplaceAllString(c.Name, "")
}

func IsVariant(c *Card) bool {
	return BaseName(c) != c.Name
}

func Supertype(c *Card) string {
	if c.Classification.Supertype == nil {
		return ""
	}
	return *c.Classification.Supertype
}

// Filter narrows a search. Zero values match everything.
type Filter struct {
	Text     string // matched against name, rules text and tags
	Type     string
	Domain   string
	Set      string
	Tag      string
	Energy   *int
	Variants bool // include every printing, not just the preferred one per card
}

func (db *DB) Search(f Filter) []*Card {
	var out []*Card
	best := map[string]int{} // base name -> index in out
	for i := range db.Cards {
		c := &db.Cards[i]
		if !f.matches(c) {
			continue
		}
		if f.Variants {
			out = append(out, c)
			continue
		}
		key := BaseName(c)
		if j, ok := best[key]; !ok {
			best[key] = len(out)
			out = append(out, c)
		} else if cmpPrinting(c, out[j]) < 0 {
			out[j] = c
		}
	}
	slices.SortFunc(out, func(a, b *Card) int {
		if d := cmpEnergy(a, b); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func (f Filter) matches(c *Card) bool {
	if f.Type != "" && !strings.EqualFold(c.Classification.Type, f.Type) &&
		!strings.EqualFold(Supertype(c), f.Type) {
		return false
	}
	if f.Domain != "" && !containsFold(c.Classification.Domain, f.Domain) {
		return false
	}
	if f.Set != "" && !strings.EqualFold(c.Set.SetID, f.Set) {
		return false
	}
	if f.Tag != "" && !containsFold(c.Tags, f.Tag) {
		return false
	}
	if f.Energy != nil && (c.Attributes.Energy == nil || *c.Attributes.Energy != *f.Energy) {
		return false
	}
	if f.Text != "" {
		q := strings.ToLower(f.Text)
		hay := strings.ToLower(c.Name + "\n" + c.Text.Plain + "\n" + strings.Join(c.Tags, " "))
		if !strings.Contains(hay, q) {
			return false
		}
	}
	return true
}

func cmpEnergy(a, b *Card) int {
	ea, eb := -1, -1
	if a.Attributes.Energy != nil {
		ea = *a.Attributes.Energy
	}
	if b.Attributes.Energy != nil {
		eb = *b.Attributes.Energy
	}
	return ea - eb
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.EqualFold(v, s) })
}

// Summary is a one-line description of a card for listings.
func Summary(c *Card) string {
	var stats []string
	if c.Attributes.Energy != nil {
		stats = append(stats, strconv.Itoa(*c.Attributes.Energy)+"E")
	}
	if c.Attributes.Power != nil {
		stats = append(stats, strconv.Itoa(*c.Attributes.Power)+"P")
	}
	if c.Attributes.Might != nil {
		stats = append(stats, strconv.Itoa(*c.Attributes.Might)+"M")
	}
	kind := c.Classification.Type
	if st := Supertype(c); st != "" {
		kind = st + " " + kind
	}
	return fmt.Sprintf("%-14s %-38s %-8s %-18s %s",
		c.RiftboundID, c.Name, strings.Join(stats, " "), kind,
		strings.Join(c.Classification.Domain, "/"))
}
