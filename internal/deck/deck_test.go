package deck

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"deckbuilder/internal/cards"
	"deckbuilder/internal/riftcodex"
)

func card(id, name, typ, super string, tags []string, domains ...string) cards.Card {
	c := cards.Card{Name: name, RiftboundID: id, Tags: tags}
	c.Classification.Type = typ
	c.Classification.Domain = domains
	if super != "" {
		c.Classification.Supertype = &super
	}
	return c
}

// testDB has a Jinx (Fury/Chaos) legend, enough Fury/Chaos units to fill a
// deck, and a few cards that break rules.
func testDB() *cards.DB {
	cs := []riftcodex.Card{
		card("leg-1", "Loose Cannon", "Legend", "", []string{"Jinx"}, "Fury", "Chaos"),
		card("chp-1", "Jinx - Rebel", "Unit", "Champion", []string{"Jinx"}, "Fury"),
		card("chp-2", "Vi - Enforcer", "Unit", "Champion", []string{"Vi"}, "Body"),
		card("sig-1", "Super Mega Death Rocket", "Spell", "Signature", []string{"Jinx"}, "Fury"),
		card("sig-2", "Vi Signature", "Spell", "Signature", []string{"Vi"}, "Fury"),
		card("body-1", "Body Unit", "Unit", "", nil, "Body"),
		card("tok-1", "Recruit", "Unit", "Token", nil, "Colorless"),
		card("run-f", "Fury Rune", "Rune", "Basic", nil, "Fury"),
		card("run-c", "Chaos Rune", "Rune", "Basic", nil, "Chaos"),
		card("run-b", "Body Rune", "Rune", "Basic", nil, "Body"),
		card("bf-1", "Field One", "Battlefield", "", nil, "Colorless"),
		card("bf-2", "Field Two", "Battlefield", "", nil, "Colorless"),
		card("bf-3", "Field Three", "Battlefield", "", nil, "Colorless"),
		card("u-1a", "Unit 1 (Alternate Art)", "Unit", "", nil, "Fury"),
	}
	for i := 1; i <= 13; i++ {
		cs = append(cs, card(fmt.Sprintf("u-%d", i), fmt.Sprintf("Unit %d", i), "Unit", "", nil, "Fury"))
	}
	return cards.New(cs, time.Now())
}

func mustAdd(t *testing.T, d *Deck, db *cards.DB, id string, n int) {
	t.Helper()
	c, ok := db.ByID(id)
	if !ok {
		t.Fatalf("no card %s", id)
	}
	if _, err := d.Add(c, n); err != nil {
		t.Fatal(err)
	}
}

func legalDeck(t *testing.T, db *cards.DB) *Deck {
	d := New("test")
	mustAdd(t, d, db, "leg-1", 1)
	c, _ := db.ByID("chp-1")
	if err := d.SetChampion(c); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 13; i++ {
		mustAdd(t, d, db, fmt.Sprintf("u-%d", i), 3)
	} // 39 + champion = 40
	mustAdd(t, d, db, "run-f", 6)
	mustAdd(t, d, db, "run-c", 6)
	for _, bf := range []string{"bf-1", "bf-2", "bf-3"} {
		mustAdd(t, d, db, bf, 1)
	}
	return d
}

func TestLegalDeck(t *testing.T) {
	db := testDB()
	if p := legalDeck(t, db).Validate(db); len(p) != 0 {
		t.Fatalf("expected legal deck, got: %v", p)
	}
}

func TestValidateProblems(t *testing.T) {
	db := testDB()
	tests := []struct {
		name   string
		modify func(d *Deck)
		want   string
	}{
		{"too few cards", func(d *Deck) { d.Main["u-1"] = 1 }, "needs at least 40"},
		{"variant copies count together", func(d *Deck) { d.Main["u-1a"] = 1 }, "4 copies of Unit 1"},
		{"off-domain", func(d *Deck) { d.Main["body-1"] = 1 }, "outside the legend's domains"},
		{"off-domain rune", func(d *Deck) { d.Runes["run-f"] = 5; d.Runes["run-b"] = 1 }, "Body Rune is outside"},
		{"wrong rune count", func(d *Deck) { d.Runes["run-f"] = 5 }, "needs exactly 12"},
		{"wrong champion", func(d *Deck) { d.Champion = "chp-2" }, "does not match legend"},
		{"no legend", func(d *Deck) { d.Legend = "" }, "no legend chosen"},
		{"too many signatures", func(d *Deck) { d.Main["sig-1"] = 4 }, "4 signature cards"},
		{"other champion's signature", func(d *Deck) { d.Main["sig-2"] = 1 }, "signature card Vi Signature does not match"},
		{"token", func(d *Deck) { d.Main["tok-1"] = 1 }, "token"},
		{"two battlefields", func(d *Deck) { d.Battlefields = d.Battlefields[:2] }, "2 battlefields"},
		{"duplicate battlefield", func(d *Deck) { d.Battlefields[2] = "bf-1" }, "more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := legalDeck(t, db)
			tt.modify(d)
			problems := d.Validate(db)
			for _, p := range problems {
				if strings.Contains(p, tt.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got %v", tt.want, problems)
		})
	}
}

func TestSetChampionMovesCopy(t *testing.T) {
	db := testDB()
	d := New("x")
	mustAdd(t, d, db, "chp-1", 2)
	c, _ := db.ByID("chp-1")
	if err := d.SetChampion(c); err != nil {
		t.Fatal(err)
	}
	if d.Main["chp-1"] != 1 || d.MainCount() != 2 {
		t.Fatalf("main=%v count=%d", d.Main, d.MainCount())
	}
	if c, _ := db.ByID("u-1"); d.SetChampion(c) == nil {
		t.Fatal("expected error setting a non-champion as champion")
	}
}

func TestRemove(t *testing.T) {
	db := testDB()
	d := legalDeck(t, db)
	u1, _ := db.ByID("u-1")
	if err := d.Remove(u1, 2); err != nil || d.Main["u-1"] != 1 {
		t.Fatalf("err=%v count=%d", err, d.Main["u-1"])
	}
	if err := d.Remove(u1, 0); err != nil || d.Main["u-1"] != 0 {
		t.Fatalf("err=%v count=%d", err, d.Main["u-1"])
	}
	if err := d.Remove(u1, 1); err == nil {
		t.Fatal("expected error removing absent card")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	db := testDB()
	s := Store{Dir: t.TempDir()}
	d := legalDeck(t, db)
	d.Name = "My Jinx Deck!"
	if err := s.Save(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("my jinx deck!")
	if err != nil {
		t.Fatal(err)
	}
	if got.MainCount() != 40 || got.Legend != "leg-1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Fatalf("List() = %d decks", len(list))
	}
}
