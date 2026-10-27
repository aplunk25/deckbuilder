package riftcodex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDecodeFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/cards_page.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Page
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 100 || p.Total == 0 {
		t.Fatalf("got %d items, total %d", len(p.Items), p.Total)
	}
	c := p.Items[1]
	if c.Name != "Bewitching Spirit" || c.Attributes.Energy == nil || *c.Attributes.Energy != 3 {
		t.Fatalf("unexpected card: %+v", c)
	}
}

func TestAllCardsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		json.NewEncoder(w).Encode(Page{
			Items: []Card{{Name: "card " + page}},
			Total: 3, Pages: 3,
		})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	cards, err := c.AllCards(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("got %d cards, want 3", len(cards))
	}
	for i, card := range cards {
		if want := fmt.Sprintf("card %d", i+1); card.Name != want {
			t.Errorf("cards[%d] = %q, want %q", i, card.Name, want)
		}
	}
}
