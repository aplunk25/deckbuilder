// Package deck models a Riftbound constructed deck and checks it against the
// deckbuilding rules.
package deck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"deckbuilder/internal/cards"
)

const (
	MainDeckMin     = 40
	RuneDeckSize    = 12
	BattlefieldSize = 3
	MaxCopies       = 3
	MaxSignatures   = 3
)

// Deck stores cards by Riftbound ID. The chosen champion is kept separately
// from Main but counts toward the main deck size and copy limits.
type Deck struct {
	Name         string         `json:"name"`
	Legend       string         `json:"legend,omitempty"`
	Champion     string         `json:"champion,omitempty"`
	Main         map[string]int `json:"main"`
	Runes        map[string]int `json:"runes"`
	Battlefields []string       `json:"battlefields"`
}

func New(name string) *Deck {
	return &Deck{Name: name, Main: map[string]int{}, Runes: map[string]int{}}
}

// Zone names which part of the deck a card belongs in.
type Zone string

const (
	ZoneLegend      Zone = "legend"
	ZoneMain        Zone = "main"
	ZoneRunes       Zone = "runes"
	ZoneBattlefield Zone = "battlefields"
)

// ZoneFor returns where a card goes based on its type.
func ZoneFor(c *cards.Card) Zone {
	switch c.Classification.Type {
	case "Legend":
		return ZoneLegend
	case "Rune":
		return ZoneRunes
	case "Battlefield":
		return ZoneBattlefield
	default:
		return ZoneMain
	}
}

// Add puts n copies of c into the appropriate zone. Legends replace the
// current legend; battlefields are added once regardless of n.
func (d *Deck) Add(c *cards.Card, n int) (Zone, error) {
	if n < 1 {
		return "", fmt.Errorf("count must be at least 1")
	}
	z := ZoneFor(c)
	switch z {
	case ZoneLegend:
		d.Legend = c.RiftboundID
	case ZoneRunes:
		d.Runes[c.RiftboundID] += n
	case ZoneBattlefield:
		if slices.Contains(d.Battlefields, c.RiftboundID) {
			return z, fmt.Errorf("%s is already in the deck", c.Name)
		}
		d.Battlefields = append(d.Battlefields, c.RiftboundID)
	default:
		d.Main[c.RiftboundID] += n
	}
	return z, nil
}

// Remove takes up to n copies of c out of the deck (n <= 0 removes all).
func (d *Deck) Remove(c *cards.Card, n int) error {
	id := c.RiftboundID
	switch ZoneFor(c) {
	case ZoneLegend:
		if d.Legend != id {
			return fmt.Errorf("%s is not the deck's legend", c.Name)
		}
		d.Legend = ""
		return nil
	case ZoneBattlefield:
		i := slices.Index(d.Battlefields, id)
		if i < 0 {
			return fmt.Errorf("%s is not in the deck", c.Name)
		}
		d.Battlefields = slices.Delete(d.Battlefields, i, i+1)
		return nil
	}
	if d.Champion == id && d.Main[id] == 0 {
		d.Champion = ""
		return nil
	}
	m := d.Main
	if ZoneFor(c) == ZoneRunes {
		m = d.Runes
	}
	if m[id] == 0 {
		return fmt.Errorf("%s is not in the deck", c.Name)
	}
	if n <= 0 || n >= m[id] {
		delete(m, id)
	} else {
		m[id] -= n
	}
	return nil
}

// SetChampion makes c the chosen champion. If a copy of c is already in the
// main deck, one copy moves over so the total count is unchanged.
func (d *Deck) SetChampion(c *cards.Card) error {
	if c.Classification.Type != "Unit" || cards.Supertype(c) != "Champion" {
		return fmt.Errorf("%s is not a champion unit", c.Name)
	}
	if d.Champion != "" {
		d.Main[d.Champion]++
	}
	d.Champion = c.RiftboundID
	if d.Main[c.RiftboundID] > 0 {
		d.Main[c.RiftboundID]--
		if d.Main[c.RiftboundID] == 0 {
			delete(d.Main, c.RiftboundID)
		}
	}
	return nil
}

// MainCount is the number of cards in the main deck, including the champion.
func (d *Deck) MainCount() int {
	n := sum(d.Main)
	if d.Champion != "" {
		n++
	}
	return n
}

func (d *Deck) RuneCount() int { return sum(d.Runes) }

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// Validate returns every rule the deck breaks. An empty result means the deck
// is tournament legal.
func (d *Deck) Validate(db *cards.DB) []string {
	var problems []string
	bad := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	lookup := func(id string) *cards.Card {
		c, ok := db.ByID(id)
		if !ok {
			bad("unknown card %s (try `sync`)", id)
		}
		return c
	}

	// Legend: defines the domain identity and the champion.
	var legend *cards.Card
	if d.Legend == "" {
		bad("no legend chosen")
	} else if legend = lookup(d.Legend); legend != nil && legend.Classification.Type != "Legend" {
		bad("%s is not a legend", legend.Name)
		legend = nil
	}
	allowed := func(c *cards.Card) bool {
		if legend == nil {
			return true
		}
		for _, dom := range c.Classification.Domain {
			if dom != "Colorless" && !slices.Contains(legend.Classification.Domain, dom) {
				return false
			}
		}
		return true
	}
	sharesTag := func(c *cards.Card) bool {
		return legend == nil || slices.ContainsFunc(c.Tags, func(t string) bool {
			return slices.Contains(legend.Tags, t)
		})
	}

	// Chosen champion.
	if d.Champion == "" {
		bad("no chosen champion")
	} else if c := lookup(d.Champion); c != nil {
		if cards.Supertype(c) != "Champion" || c.Classification.Type != "Unit" {
			bad("chosen champion %s is not a champion unit", c.Name)
		} else if !sharesTag(c) {
			bad("chosen champion %s does not match legend %s", c.Name, legend.Name)
		}
	}

	// Main deck.
	if n := d.MainCount(); n < MainDeckMin {
		bad("main deck has %d cards; needs at least %d", n, MainDeckMin)
	}
	copies := map[string]int{}
	signatures := 0
	countCard := func(c *cards.Card, n int) {
		copies[cards.BaseName(c)] += n
		if cards.Supertype(c) == "Signature" {
			signatures += n
			if !sharesTag(c) {
				bad("signature card %s does not match legend %s", c.Name, legend.Name)
			}
		}
		if !allowed(c) {
			bad("%s (%s) is outside the legend's domains (%s)", c.Name,
				strings.Join(c.Classification.Domain, "/"),
				strings.Join(legend.Classification.Domain, "/"))
		}
	}
	for _, id := range sortedKeys(d.Main) {
		c := lookup(id)
		if c == nil {
			continue
		}
		switch {
		case ZoneFor(c) != ZoneMain:
			bad("%s (%s) cannot be in the main deck", c.Name, c.Classification.Type)
		case cards.Supertype(c) == "Token":
			bad("%s is a token and cannot be in a deck", c.Name)
		}
		countCard(c, d.Main[id])
	}
	if d.Champion != "" {
		if c, ok := db.ByID(d.Champion); ok {
			countCard(c, 1)
		}
	}
	for _, name := range sortedKeys(copies) {
		if copies[name] > MaxCopies {
			bad("%d copies of %s; max is %d", copies[name], name, MaxCopies)
		}
	}
	if signatures > MaxSignatures {
		bad("%d signature cards; max is %d", signatures, MaxSignatures)
	}

	// Rune deck.
	if n := d.RuneCount(); n != RuneDeckSize {
		bad("rune deck has %d runes; needs exactly %d", n, RuneDeckSize)
	}
	for _, id := range sortedKeys(d.Runes) {
		if c := lookup(id); c != nil && !allowed(c) {
			bad("%s is outside the legend's domains", c.Name)
		}
	}

	// Battlefields.
	if n := len(d.Battlefields); n != BattlefieldSize {
		bad("%d battlefields; needs exactly %d", n, BattlefieldSize)
	}
	seen := map[string]bool{}
	for _, id := range d.Battlefields {
		if c := lookup(id); c != nil {
			if seen[cards.BaseName(c)] {
				bad("battlefield %s is included more than once", cards.BaseName(c))
			}
			seen[cards.BaseName(c)] = true
		}
	}
	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Store saves decks as JSON files in a directory.
type Store struct{ Dir string }

var unsafeChars = regexp.MustCompile(`[^a-z0-9._-]+`)

func (s Store) path(name string) string {
	slug := strings.Trim(unsafeChars.ReplaceAllString(strings.ToLower(name), "-"), "-.")
	return filepath.Join(s.Dir, slug+".json")
}

func (s Store) Exists(name string) bool {
	_, err := os.Stat(s.path(name))
	return err == nil
}

func (s Store) Load(name string) (*Deck, error) {
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no deck named %q", name)
	}
	if err != nil {
		return nil, err
	}
	d := New("")
	if err := json.Unmarshal(b, d); err != nil {
		return nil, err
	}
	if d.Main == nil {
		d.Main = map[string]int{}
	}
	if d.Runes == nil {
		d.Runes = map[string]int{}
	}
	return d, nil
}

func (s Store) Save(d *Deck) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(d.Name), append(b, '\n'), 0o644)
}

func (s Store) Delete(name string) error {
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no deck named %q", name)
	}
	return err
}

func (s Store) List() ([]*Deck, error) {
	files, err := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var decks []*Deck
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		d := New("")
		if err := json.Unmarshal(b, d); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		decks = append(decks, d)
	}
	return decks, nil
}
