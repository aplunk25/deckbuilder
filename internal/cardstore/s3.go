// Package cardstore keeps the card database in S3, where the sync Lambda
// writes it and the API Lambda reads it.
package cardstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"deckbuilder/internal/cards"
)

// S3API is the subset of the S3 client used here, so tests can fake it.
type S3API interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type Store struct {
	Client S3API
	Bucket string
	Key    string
	// TTL is how long a loaded database is reused before re-reading S3.
	// Lambda keeps the process warm between requests, so this avoids
	// downloading the cards on every invocation.
	TTL time.Duration

	mu       sync.Mutex
	db       *cards.DB
	loadedAt time.Time
	now      func() time.Time
}

func (s *Store) Put(ctx context.Context, cs []cards.Card) error {
	b, err := cards.Encode(cs)
	if err != nil {
		return err
	}
	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(s.Key),
		Body:        bytes.NewReader(b),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("writing s3://%s/%s: %w", s.Bucket, s.Key, err)
	}
	return nil
}

// Cards returns the cached database, reloading it from S3 once the TTL has
// passed. If a reload fails, the stale copy is served rather than failing
// requests.
func (s *Store) Cards(ctx context.Context) (*cards.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	if s.db != nil && now().Sub(s.loadedAt) < s.TTL {
		return s.db, nil
	}
	db, err := s.load(ctx)
	if err != nil {
		if s.db != nil {
			return s.db, nil
		}
		return nil, err
	}
	s.db, s.loadedAt = db, now()
	return db, nil
}

func (s *Store) load(ctx context.Context) (*cards.DB, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.Key),
	})
	if err != nil {
		return nil, fmt.Errorf("reading s3://%s/%s: %w", s.Bucket, s.Key, err)
	}
	defer out.Body.Close()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}
	return cards.Decode(b)
}
