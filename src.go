// Command deckbuilder is a command-line Riftbound deckbuilder backed by the
// Riftcodex card API (https://riftcodex.com).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"deckbuilder/internal/cards"
	"deckbuilder/internal/deck"
	"deckbuilder/internal/riftcodex"
)

const usage = `deckbuilder - Riftbound deckbuilder (card data from riftcodex.com)

Usage:
  deckbuilder version                           print the version
  deckbuilder sync                              download/refresh the card database
  deckbuilder search [text] [flags]             search cards
      -type T    Unit, Spell, Gear, Legend, Rune, Battlefield, Champion, Signature
      -domain D  Fury, Calm, Mind, Body, Chaos, Order, Colorless
      -set S     set code, e.g. OGN, SFD, UNL
      -tag T     tag, e.g. Jinx, Piltover
      -energy N  energy cost
      -all       include alternate art / overnumbered printings
      -limit N   max results (default 50, 0 = no limit)
  deckbuilder card <card>                       show card details

  deckbuilder deck list                         list saved decks
  deckbuilder deck new <deck> [legend]          create a deck
  deckbuilder deck show <deck>                  show a deck and its legality
  deckbuilder deck add <deck> <card> [count]    add cards (legend, runes and battlefields
                                                go to the right place automatically)
  deckbuilder deck rm <deck> <card> [count]     remove cards (all copies if no count)
  deckbuilder deck champion <deck> <card>       set the chosen champion
  deckbuilder deck check <deck> [--remote]      check deckbuilding rules (--remote uses
                                                the API at $RIFTBOUND_API_URL)
  deckbuilder deck export <deck>                print a plain-text decklist
  deckbuilder deck delete <deck>                delete a deck

A <card> is a Riftbound ID (e.g. ogn-001-298), a card name, or part of one.
Quote names containing spaces. Data lives in $RIFTBOUND_HOME (default ~/.riftbound).
`

// Set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errIllegal) { // problems were already printed
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Printf("deckbuilder %s (%s)\n", version, commit)
		return nil
	}
	app, err := newApp()
	if err != nil {
		return err
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "sync":
		return app.sync()
	case "search":
		return app.search(rest)
	case "card":
		return app.card(rest)
	case "deck":
		return app.deck(rest)
	}
	return fmt.Errorf("unknown command %q (see `deckbuilder help`)", cmd)
}

type app struct {
	home  string
	decks deck.Store
	db    *cards.DB
}

func newApp() (*app, error) {
	home := os.Getenv("RIFTBOUND_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(h, ".riftbound")
	}
	return &app{home: home, decks: deck.Store{Dir: filepath.Join(home, "decks")}}, nil
}

func (a *app) cachePath() string { return filepath.Join(a.home, "cards.json") }

// cards loads the card database, syncing first if there is no local cache.
func (a *app) cards() (*cards.DB, error) {
	if a.db != nil {
		return a.db, nil
	}
	db, err := cards.Load(a.cachePath())
	if errors.Is(err, cards.ErrNoCache) {
		fmt.Fprintln(os.Stderr, "No local card database yet; syncing from riftcodex.com...")
		if err := a.sync(); err != nil {
			return nil, err
		}
		db, err = cards.Load(a.cachePath())
	}
	if err != nil {
		return nil, err
	}
	a.db = db
	return db, nil
}

func (a *app) sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	all, err := riftcodex.NewClient().AllCards(ctx, func(got, total int) {
		fmt.Fprintf(os.Stderr, "\rfetched %d/%d cards", got, total)
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	if err := cards.Save(a.cachePath(), all); err != nil {
		return err
	}
	a.db = nil
	fmt.Printf("Saved %d cards to %s\n", len(all), a.cachePath())
	return nil
}

// parseInterspersed lets flags appear before, after or between positional
// arguments, which the flag package doesn't allow on its own.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func (a *app) search(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	var f cards.Filter
	fs.StringVar(&f.Type, "type", "", "")
	fs.StringVar(&f.Domain, "domain", "", "")
	fs.StringVar(&f.Set, "set", "", "")
	fs.StringVar(&f.Tag, "tag", "", "")
	fs.BoolVar(&f.Variants, "all", false, "")
	energy := fs.Int("energy", -1, "")
	limit := fs.Int("limit", 50, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	f.Text = strings.Join(pos, " ")
	if *energy >= 0 {
		f.Energy = energy
	}
	db, err := a.cards()
	if err != nil {
		return err
	}
	results := db.Search(f)
	shown := results
	if *limit > 0 && len(shown) > *limit {
		shown = shown[:*limit]
	}
	for _, c := range shown {
		fmt.Println(cards.Summary(c))
	}
	if len(shown) < len(results) {
		fmt.Printf("... %d of %d results shown (use -limit 0 for all)\n", len(shown), len(results))
	} else {
		fmt.Printf("%d results\n", len(results))
	}
	return nil
}

func (a *app) card(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deckbuilder card <card>")
	}
	db, err := a.cards()
	if err != nil {
		return err
	}
	c, err := db.Resolve(strings.Join(args, " "))
	if err != nil {
		return err
	}
	printCard(c)
	return nil
}

func printCard(c *cards.Card) {
	kind := c.Classification.Type
	if st := cards.Supertype(c); st != "" {
		kind = st + " " + kind
	}
	fmt.Println(c.Name)
	fmt.Printf("  %s · %s · %s\n", kind, strings.Join(c.Classification.Domain, "/"), c.Classification.Rarity)
	attr := c.Attributes
	var stats []string
	for _, s := range []struct {
		label string
		v     *int
	}{{"Energy", attr.Energy}, {"Power", attr.Power}, {"Might", attr.Might}} {
		if s.v != nil {
			stats = append(stats, fmt.Sprintf("%s %d", s.label, *s.v))
		}
	}
	if len(stats) > 0 {
		fmt.Println("  " + strings.Join(stats, " · "))
	}
	if c.Text.Plain != "" {
		fmt.Println("\n  " + c.Text.Plain)
	}
	if c.Text.Flavour != nil && *c.Text.Flavour != "" {
		fmt.Printf("\n  \"%s\"\n", *c.Text.Flavour)
	}
	fmt.Println()
	if len(c.Tags) > 0 {
		fmt.Println("  Tags:   " + strings.Join(c.Tags, ", "))
	}
	fmt.Printf("  Set:    %s (%s) #%d\n", c.Set.Label, c.Set.SetID, c.CollectorNumber)
	fmt.Println("  ID:     " + c.RiftboundID)
	fmt.Println("  Artist: " + c.Media.Artist)
	fmt.Println("  Image:  " + c.Media.ImageURL)
}

func (a *app) deck(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deckbuilder deck <list|new|show|add|rm|champion|check|export|delete> <deck>")
	}
	sub, args := args[0], args[1:]
	if sub == "list" || sub == "ls" {
		return a.deckList()
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: deckbuilder deck %s <deck> [args]", sub)
	}
	name, args := args[0], args[1:]

	switch sub {
	case "new":
		return a.deckNew(name, args)
	case "delete":
		if err := a.decks.Delete(name); err != nil {
			return err
		}
		fmt.Printf("Deleted %q\n", name)
		return nil
	}

	d, err := a.decks.Load(name)
	if err != nil {
		return err
	}
	if sub == "check" && slices.Contains(args, "--remote") {
		return remoteCheck(d)
	}
	db, err := a.cards()
	if err != nil {
		return err
	}
	switch sub {
	case "show":
		a.printDeck(d, db)
		return nil
	case "check":
		return printProblems(d.Validate(db))
	case "export":
		printExport(d, db)
		return nil
	case "add", "rm", "remove":
		ref, n, err := cardAndCount(args)
		if err != nil {
			return err
		}
		c, err := db.Resolve(ref)
		if err != nil {
			return err
		}
		if sub == "add" {
			if n == 0 {
				n = 1
			}
			z, err := d.Add(c, n)
			if err != nil {
				return err
			}
			if z == deck.ZoneLegend {
				fmt.Printf("Legend set to %s\n", c.Name)
			} else {
				fmt.Printf("Added %d× %s to %s\n", n, c.Name, z)
			}
		} else {
			if err := d.Remove(c, n); err != nil {
				return err
			}
			fmt.Printf("Removed %s\n", c.Name)
		}
	case "champion":
		if len(args) == 0 {
			return errors.New("usage: deckbuilder deck champion <deck> <card>")
		}
		c, err := db.Resolve(strings.Join(args, " "))
		if err != nil {
			return err
		}
		if err := d.SetChampion(c); err != nil {
			return err
		}
		fmt.Printf("Chosen champion set to %s\n", c.Name)
	default:
		return fmt.Errorf("unknown deck command %q", sub)
	}
	if err := a.decks.Save(d); err != nil {
		return err
	}
	fmt.Printf("%s: %d/%d main, %d/%d runes, %d/%d battlefields\n", d.Name,
		d.MainCount(), deck.MainDeckMin, d.RuneCount(), deck.RuneDeckSize,
		len(d.Battlefields), deck.BattlefieldSize)
	return nil
}

// cardAndCount splits "<card words...> [count]". A trailing integer is the
// count (0 if absent).
func cardAndCount(args []string) (string, int, error) {
	if len(args) == 0 {
		return "", 0, errors.New("missing card")
	}
	if len(args) > 1 {
		if n, err := strconv.Atoi(args[len(args)-1]); err == nil {
			if n < 1 {
				return "", 0, errors.New("count must be at least 1")
			}
			return strings.Join(args[:len(args)-1], " "), n, nil
		}
	}
	return strings.Join(args, " "), 0, nil
}

func (a *app) deckNew(name string, args []string) error {
	if a.decks.Exists(name) {
		return fmt.Errorf("a deck named %q already exists", name)
	}
	d := deck.New(name)
	if len(args) > 0 {
		db, err := a.cards()
		if err != nil {
			return err
		}
		c, err := db.Resolve(strings.Join(args, " "))
		if err != nil {
			return err
		}
		if c.Classification.Type != "Legend" {
			return fmt.Errorf("%s is not a legend", c.Name)
		}
		d.Legend = c.RiftboundID
	}
	if err := a.decks.Save(d); err != nil {
		return err
	}
	fmt.Printf("Created deck %q\n", name)
	return nil
}

func (a *app) deckList() error {
	decks, err := a.decks.List()
	if err != nil {
		return err
	}
	if len(decks) == 0 {
		fmt.Println("No decks yet. Create one with `deckbuilder deck new <name>`.")
		return nil
	}
	db, _ := a.cards()
	for _, d := range decks {
		legend := "(no legend)"
		if db != nil {
			if c, ok := db.ByID(d.Legend); ok {
				legend = c.Name + " [" + strings.Join(c.Tags, ", ") + "]"
			}
		}
		fmt.Printf("%-24s %-40s %d cards\n", d.Name, legend, d.MainCount())
	}
	return nil
}

type entry struct {
	card  *cards.Card
	count int
}

func entries(m map[string]int, db *cards.DB) []entry {
	var out []entry
	for id, n := range m {
		if c, ok := db.ByID(id); ok {
			out = append(out, entry{c, n})
		}
	}
	slices.SortFunc(out, func(a, b entry) int {
		ea, eb := energy(a.card), energy(b.card)
		if ea != eb {
			return ea - eb
		}
		return strings.Compare(a.card.Name, b.card.Name)
	})
	return out
}

func energy(c *cards.Card) int {
	if c.Attributes.Energy == nil {
		return -1
	}
	return *c.Attributes.Energy
}

func (a *app) printDeck(d *deck.Deck, db *cards.DB) {
	fmt.Printf("== %s ==\n\n", d.Name)
	if c, ok := db.ByID(d.Legend); ok {
		fmt.Printf("Legend:   %s [%s] (%s)\n", c.Name, strings.Join(c.Tags, ", "),
			strings.Join(c.Classification.Domain, "/"))
	} else {
		fmt.Println("Legend:   (none)")
	}
	if c, ok := db.ByID(d.Champion); ok {
		fmt.Printf("Champion: %s\n", c.Name)
	} else {
		fmt.Println("Champion: (none)")
	}

	fmt.Printf("\nMain deck (%d)\n", d.MainCount())
	byType := map[string][]entry{}
	for _, e := range entries(d.Main, db) {
		byType[e.card.Classification.Type] = append(byType[e.card.Classification.Type], e)
	}
	for _, t := range []string{"Unit", "Spell", "Gear"} {
		es := byType[t]
		if len(es) == 0 {
			continue
		}
		total := 0
		for _, e := range es {
			total += e.count
		}
		fmt.Printf("  %ss (%d)\n", t, total)
		for _, e := range es {
			cost := " "
			if e.card.Attributes.Energy != nil {
				cost = strconv.Itoa(*e.card.Attributes.Energy)
			}
			fmt.Printf("    %d× [%s] %-36s %s\n", e.count, cost, e.card.Name,
				strings.Join(e.card.Classification.Domain, "/"))
		}
	}

	fmt.Printf("\nRunes (%d)\n", d.RuneCount())
	for _, e := range entries(d.Runes, db) {
		fmt.Printf("    %d× %s\n", e.count, e.card.Name)
	}
	fmt.Printf("\nBattlefields (%d)\n", len(d.Battlefields))
	for _, id := range d.Battlefields {
		if c, ok := db.ByID(id); ok {
			fmt.Printf("    %s\n", c.Name)
		}
	}
	fmt.Println()
	_ = printProblems(d.Validate(db)) // show reports problems but doesn't fail
}

var errIllegal = errors.New("deck is not legal")

// printProblems prints a validation result and returns errIllegal if there
// are problems, so `deck check` exits non-zero for illegal decks.
func printProblems(problems []string) error {
	if len(problems) == 0 {
		fmt.Println("✓ Deck is legal")
		return nil
	}
	fmt.Printf("✗ %d problem(s):\n", len(problems))
	for _, p := range problems {
		fmt.Println("  - " + p)
	}
	return errIllegal
}

// remoteCheck validates a deck with the deployed API instead of locally.
func remoteCheck(d *deck.Deck) error {
	base := strings.TrimRight(os.Getenv("RIFTBOUND_API_URL"), "/")
	if base == "" {
		return errors.New("set RIFTBOUND_API_URL to use --remote")
	}
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(base+"/decks/validate", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Problems []string `json:"problems"`
		Error    string   `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("decoding API response (%s): %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (%s): %s", resp.Status, out.Error)
	}
	return printProblems(out.Problems)
}

func printExport(d *deck.Deck, db *cards.DB) {
	if c, ok := db.ByID(d.Legend); ok {
		fmt.Println("Legend:")
		fmt.Printf("1 %s\n\n", c.Name)
	}
	if c, ok := db.ByID(d.Champion); ok {
		fmt.Println("Champion:")
		fmt.Printf("1 %s\n\n", c.Name)
	}
	fmt.Println("MainDeck:")
	for _, e := range entries(d.Main, db) {
		fmt.Printf("%d %s\n", e.count, e.card.Name)
	}
	fmt.Println("\nBattlefields:")
	for _, id := range d.Battlefields {
		if c, ok := db.ByID(id); ok {
			fmt.Printf("1 %s\n", c.Name)
		}
	}
	fmt.Println("\nRunes:")
	for _, e := range entries(d.Runes, db) {
		fmt.Printf("%d %s\n", e.count, e.card.Name)
	}
}
