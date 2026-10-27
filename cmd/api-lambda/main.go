// Command api-lambda serves the card/deck HTTP API. In AWS it runs behind an
// API Gateway HTTP API; anywhere else it starts a normal HTTP server, which is
// handy for local development.
//
// Environment:
//
//	CARDS_BUCKET  S3 bucket holding cards.json (used in AWS)
//	CARDS_KEY     object key (default cards.json)
//	CARDS_FILE    read cards from a local cache file instead of S3
//	PORT          local server port (default 8080)
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"deckbuilder/internal/api"
	"deckbuilder/internal/cards"
	"deckbuilder/internal/cardstore"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	src, err := cardSource()
	if err != nil {
		slog.Error("configuring card source", "err", err)
		os.Exit(1)
	}
	handler := api.NewHandler(src)

	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
		return
	}

	addr := ":" + envOr("PORT", "8080")
	slog.Info("listening", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func cardSource() (api.CardSource, error) {
	if path := os.Getenv("CARDS_FILE"); path != "" {
		db, err := cards.Load(path)
		if err != nil {
			return nil, err
		}
		return func(context.Context) (*cards.DB, error) { return db, nil }, nil
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, err
	}
	store := &cardstore.Store{
		Client: s3.NewFromConfig(cfg),
		Bucket: os.Getenv("CARDS_BUCKET"),
		Key:    envOr("CARDS_KEY", "cards.json"),
		TTL:    15 * time.Minute,
	}
	return store.Cards, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
