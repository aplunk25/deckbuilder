// Command sync-lambda is invoked on a schedule. It downloads every card from
// the Riftcodex API and writes them to S3 for the API Lambda to serve.
//
// Environment:
//
//	CARDS_BUCKET  S3 bucket to write to (required)
//	CARDS_KEY     object key (default cards.json)
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"deckbuilder/internal/cardstore"
	"deckbuilder/internal/riftcodex"
)

// minCards guards against overwriting good data with a truncated or empty
// response if the upstream API misbehaves.
const minCards = 100

type result struct {
	Cards int `json:"cards"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("loading AWS config", "err", err)
		os.Exit(1)
	}
	store := &cardstore.Store{
		Client: s3.NewFromConfig(cfg),
		Bucket: os.Getenv("CARDS_BUCKET"),
		Key:    envOr("CARDS_KEY", "cards.json"),
	}
	if store.Bucket == "" {
		slog.Error("CARDS_BUCKET is not set")
		os.Exit(1)
	}
	client := riftcodex.NewClient()

	lambda.Start(func(ctx context.Context) (result, error) {
		all, err := client.AllCards(ctx, nil)
		if err != nil {
			return result{}, err
		}
		if len(all) < minCards {
			return result{}, errors.New("upstream returned too few cards; refusing to overwrite")
		}
		if err := store.Put(ctx, all); err != nil {
			return result{}, err
		}
		slog.InfoContext(ctx, "synced cards", "count", len(all), "bucket", store.Bucket, "key", store.Key)
		return result{Cards: len(all)}, nil
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
