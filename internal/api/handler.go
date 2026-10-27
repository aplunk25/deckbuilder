// Package api is the HTTP API for card search and deck validation. It is a
// plain net/http handler so it can run behind API Gateway (via the Lambda
// adapter) or as a regular local server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"deckbuilder/internal/cards"
	"deckbuilder/internal/deck"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	maxBodyBytes = 64 << 10
)

// CardSource returns the current card database.
type CardSource func(ctx context.Context) (*cards.DB, error)

type server struct {
	cards CardSource
}

func NewHandler(src CardSource) http.Handler {
	s := &server{cards: src}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /cards", s.search)
	mux.HandleFunc("GET /cards/{ref}", s.card)
	mux.HandleFunc("POST /decks/validate", s.validate)
	return logRequests(mux)
}

type healthResponse struct {
	Status   string    `json:"status"`
	Cards    int       `json:"cards"`
	SyncedAt time.Time `json:"synced_at"`
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	db, ok := s.db(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Cards: len(db.Cards), SyncedAt: db.SyncedAt})
}

type searchResponse struct {
	Total int           `json:"total"`
	Items []*cards.Card `json:"items"`
}

// search: GET /cards?q=&type=&domain=&set=&tag=&energy=&all=&limit=
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := cards.Filter{
		Text:   q.Get("q"),
		Type:   q.Get("type"),
		Domain: q.Get("domain"),
		Set:    q.Get("set"),
		Tag:    q.Get("tag"),
	}
	if v := q.Get("all"); v != "" {
		all, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "all must be true or false")
			return
		}
		f.Variants = all
	}
	if v := q.Get("energy"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "energy must be an integer")
			return
		}
		f.Energy = &n
	}
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and "+strconv.Itoa(maxLimit))
			return
		}
		limit = n
	}

	db, ok := s.db(w, r)
	if !ok {
		return
	}
	results := db.Search(f)
	resp := searchResponse{Total: len(results), Items: results}
	if len(resp.Items) > limit {
		resp.Items = resp.Items[:limit]
	}
	if resp.Items == nil {
		resp.Items = []*cards.Card{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// card: GET /cards/{ref} where ref is a Riftbound ID or a card name.
func (s *server) card(w http.ResponseWriter, r *http.Request) {
	db, ok := s.db(w, r)
	if !ok {
		return
	}
	c, err := db.Resolve(r.PathValue("ref"))
	switch {
	case errors.Is(err, cards.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, cards.ErrAmbiguous):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, c)
	}
}

type validateResponse struct {
	Legal        bool     `json:"legal"`
	Problems     []string `json:"problems"`
	MainCount    int      `json:"main_count"`
	RuneCount    int      `json:"rune_count"`
	Battlefields int      `json:"battlefield_count"`
}

// validate: POST /decks/validate with a deck in the same JSON format the CLI
// saves to disk.
func (s *server) validate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	d := deck.New("")
	if err := json.NewDecoder(r.Body).Decode(d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid deck JSON: "+err.Error())
		return
	}
	if d.Main == nil {
		d.Main = map[string]int{}
	}
	if d.Runes == nil {
		d.Runes = map[string]int{}
	}
	db, ok := s.db(w, r)
	if !ok {
		return
	}
	problems := d.Validate(db)
	if problems == nil {
		problems = []string{}
	}
	writeJSON(w, http.StatusOK, validateResponse{
		Legal:        len(problems) == 0,
		Problems:     problems,
		MainCount:    d.MainCount(),
		RuneCount:    d.RuneCount(),
		Battlefields: len(d.Battlefields),
	})
}

func (s *server) db(w http.ResponseWriter, r *http.Request) (*cards.DB, bool) {
	db, err := s.cards(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "loading cards", "err", err)
		writeError(w, http.StatusServiceUnavailable, "card data unavailable")
		return nil, false
	}
	return db, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.InfoContext(r.Context(), "request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}
