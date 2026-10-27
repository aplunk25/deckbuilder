// Package riftcodex is a small client for the Riftcodex Riftbound card API
// (https://api.riftcodex.com).
package riftcodex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const DefaultBaseURL = "https://api.riftcodex.com"

// maxPageSize is the largest page size the API accepts.
const maxPageSize = 100

type Card struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	RiftboundID     string         `json:"riftbound_id"`
	TCGPlayerID     string         `json:"tcgplayer_id"`
	CollectorNumber int            `json:"collector_number"`
	Attributes      Attributes     `json:"attributes"`
	Classification  Classification `json:"classification"`
	Text            Text           `json:"text"`
	Set             Set            `json:"set"`
	Media           Media          `json:"media"`
	Tags            []string       `json:"tags"`
	Orientation     string         `json:"orientation"`
	Metadata        Metadata       `json:"metadata"`
}

type Attributes struct {
	Energy *int `json:"energy"`
	Might  *int `json:"might"`
	Power  *int `json:"power"`
}

type Classification struct {
	Type      string   `json:"type"`
	Supertype *string  `json:"supertype"`
	Rarity    string   `json:"rarity"`
	Domain    []string `json:"domain"`
}

type Text struct {
	Rich    string  `json:"rich"`
	Plain   string  `json:"plain"`
	Flavour *string `json:"flavour"`
}

type Set struct {
	SetID string `json:"set_id"`
	Label string `json:"label"`
}

type Media struct {
	ImageURL          string `json:"image_url"`
	Artist            string `json:"artist"`
	AccessibilityText string `json:"accessibility_text"`
}

type Metadata struct {
	CleanName    string `json:"clean_name"`
	UpdatedOn    string `json:"updated_on"`
	AlternateArt bool   `json:"alternate_art"`
	Overnumbered bool   `json:"overnumbered"`
	Signature    bool   `json:"signature"`
}

// Page is one page of a paginated card listing.
type Page struct {
	Items []Card `json:"items"`
	Total int    `json:"total"`
	Page  int    `json:"page"`
	Size  int    `json:"size"`
	Pages int    `json:"pages"`
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func NewClient() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// CardsPage fetches a single page of /cards.
func (c *Client) CardsPage(ctx context.Context, page, size int) (*Page, error) {
	q := url.Values{}
	q.Set("page", fmt.Sprint(page))
	q.Set("size", fmt.Sprint(size))
	var p Page
	if err := c.get(ctx, "/cards?"+q.Encode(), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// AllCards walks every page of /cards. progress, if non-nil, is called after
// each page with the number of cards fetched so far and the total.
func (c *Client) AllCards(ctx context.Context, progress func(got, total int)) ([]Card, error) {
	var all []Card
	for page := 1; ; page++ {
		p, err := c.CardsPage(ctx, page, maxPageSize)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}
		all = append(all, p.Items...)
		if progress != nil {
			progress(len(all), p.Total)
		}
		if page >= p.Pages || len(p.Items) == 0 {
			return all, nil
		}
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
