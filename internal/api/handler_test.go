package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"deckbuilder/internal/cards"
)

func testCards() *cards.DB {
	mk := func(id, name, typ string, domains ...string) cards.Card {
		c := cards.Card{Name: name, RiftboundID: id}
		c.Classification.Type = typ
		c.Classification.Domain = domains
		return c
	}
	return cards.New([]cards.Card{
		mk("ogn-001", "Fury Rune", "Rune", "Fury"),
		mk("ogn-002", "Chaos Rune", "Rune", "Chaos"),
		mk("ogn-010", "Blast Corps Cadet", "Unit", "Fury"),
		mk("ogn-011", "Blast Corps Cadet (Alternate Art)", "Unit", "Fury"),
	}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func do(t *testing.T, h http.Handler, method, target, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("%s %s: content-type %q", method, target, ct)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: bad JSON %q", method, target, rec.Body.String())
	}
	return rec.Code, out
}

func handler() http.Handler {
	db := testCards()
	return NewHandler(func(context.Context) (*cards.DB, error) { return db, nil })
}

func TestHealth(t *testing.T) {
	code, body := do(t, handler(), "GET", "/health", "")
	if code != 200 || body["cards"].(float64) != 4 {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestSearch(t *testing.T) {
	h := handler()
	tests := []struct {
		target string
		code   int
		total  float64
	}{
		{"/cards", 200, 3},
		{"/cards?type=rune", 200, 2},
		{"/cards?q=cadet", 200, 1},
		{"/cards?q=cadet&all=true", 200, 2},
		{"/cards?domain=chaos", 200, 1},
		{"/cards?limit=0", 400, 0},
		{"/cards?energy=x", 400, 0},
	}
	for _, tt := range tests {
		code, body := do(t, h, "GET", tt.target, "")
		if code != tt.code {
			t.Errorf("%s: status %d, want %d (%v)", tt.target, code, tt.code, body)
			continue
		}
		if code == 200 && body["total"].(float64) != tt.total {
			t.Errorf("%s: total %v, want %v", tt.target, body["total"], tt.total)
		}
	}

	_, body := do(t, h, "GET", "/cards?limit=1", "")
	if n := len(body["items"].([]any)); n != 1 {
		t.Errorf("limit=1 returned %d items", n)
	}
}

func TestCard(t *testing.T) {
	h := handler()
	for target, want := range map[string]int{
		"/cards/ogn-010":               200,
		"/cards/Blast%20Corps%20Cadet": 200,
		"/cards/rune":                  409,
		"/cards/nope":                  404,
	} {
		if code, body := do(t, h, "GET", target, ""); code != want {
			t.Errorf("%s: status %d, want %d (%v)", target, code, want, body)
		}
	}
}

func TestValidate(t *testing.T) {
	h := handler()
	code, body := do(t, h, "POST", "/decks/validate",
		`{"name":"x","main":{"ogn-010":3},"runes":{"ogn-001":12},"battlefields":[]}`)
	if code != 200 || body["legal"] != false || body["main_count"].(float64) != 3 || body["rune_count"].(float64) != 12 {
		t.Fatalf("got %d %v", code, body)
	}
	if len(body["problems"].([]any)) == 0 {
		t.Fatal("expected problems")
	}

	if code, _ := do(t, h, "POST", "/decks/validate", "{not json"); code != 400 {
		t.Errorf("bad JSON: status %d, want 400", code)
	}
	if code, _ := do(t, h, "POST", "/decks/validate", `{"main":{`+strings.Repeat(`"a":1,`, 20000)+`"b":1}}`); code != 400 {
		t.Errorf("oversized body: status %d, want 400", code)
	}
}

func TestCardsUnavailable(t *testing.T) {
	h := NewHandler(func(context.Context) (*cards.DB, error) { return nil, errors.New("s3 down") })
	if code, _ := do(t, h, "GET", "/cards", ""); code != 503 {
		t.Fatalf("status %d, want 503", code)
	}
}
