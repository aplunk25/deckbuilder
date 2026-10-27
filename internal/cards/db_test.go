package cards

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func named(id, name, typ string) Card {
	c := Card{Name: name, RiftboundID: id}
	c.Classification.Type = typ
	return c
}

func promo(id, name string) Card {
	c := named(id, name, "Unit")
	c.Classification.Rarity = "Promo"
	return c
}

func TestResolve(t *testing.T) {
	db := New([]Card{
		promo("0-promo", "Poppy - Paragon"),
		named("a-1", "Poppy - Paragon", "Unit"),
		named("a-1a", "Poppy - Paragon (Alternate Art)", "Unit"),
		named("b-1", "Fury Rune", "Rune"),
		named("b-2", "Chaos Rune", "Rune"),
	}, time.Now())

	for ref, want := range map[string]string{
		"A-1":                             "a-1",
		"poppy - paragon":                 "a-1",
		"poppy":                           "a-1",
		"Poppy - Paragon (Alternate Art)": "a-1a",
		"fury":                            "b-1",
	} {
		c, err := db.Resolve(ref)
		if err != nil || c.RiftboundID != want {
			t.Errorf("Resolve(%q) = %v, %v; want %s", ref, c, err, want)
		}
	}
	if _, err := db.Resolve("rune"); !errors.Is(err, ErrAmbiguous) {
		t.Errorf("Resolve(rune) err = %v, want ambiguous", err)
	}
	if _, err := db.Resolve("zzz"); !errors.Is(err, ErrNotFound) {
		t.Error("Resolve(zzz) should fail")
	}
}

func TestSearchCollapsesPrintings(t *testing.T) {
	db := New([]Card{
		named("a-1", "Poppy - Paragon", "Unit"),
		named("a-1a", "Poppy - Paragon (Alternate Art)", "Unit"),
		promo("p-1", "Poppy - Paragon"),
	}, time.Now())
	if r := db.Search(Filter{Text: "poppy"}); len(r) != 1 || r[0].RiftboundID != "a-1" {
		t.Errorf("got %v, want only a-1", r)
	}
	if n := len(db.Search(Filter{Text: "poppy", Variants: true})); n != 3 {
		t.Errorf("got %d results with variants, want 3", n)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cards.json")
	if _, err := Load(path); !errors.Is(err, ErrNoCache) {
		t.Fatalf("Load missing = %v, want ErrNoCache", err)
	}
	if err := Save(path, []Card{named("a-1", "X", "Unit")}); err != nil {
		t.Fatal(err)
	}
	db, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.ByID("a-1"); !ok || db.SyncedAt.IsZero() {
		t.Fatalf("bad cache: %+v", db)
	}
}
