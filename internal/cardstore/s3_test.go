package cardstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"deckbuilder/internal/cards"
)

// fakeS3 stores a single object in memory.
type fakeS3 struct {
	body []byte
	gets int
	fail bool
}

func (f *fakeS3) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gets++
	if f.fail || f.body == nil {
		return nil, errors.New("boom")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body))}, nil
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, err := io.ReadAll(in.Body)
	f.body = b
	return &s3.PutObjectOutput{}, err
}

func TestPutThenCardsCaches(t *testing.T) {
	ctx := context.Background()
	fake := &fakeS3{}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &Store{Client: fake, Bucket: "b", Key: "cards.json", TTL: time.Minute,
		now: func() time.Time { return clock }}

	if _, err := s.Cards(ctx); err == nil {
		t.Fatal("expected error before anything is stored")
	}
	if err := s.Put(ctx, []cards.Card{{RiftboundID: "x-1", Name: "X"}}); err != nil {
		t.Fatal(err)
	}

	db, err := s.Cards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.ByID("x-1"); !ok {
		t.Fatal("card missing after round trip")
	}
	gets := fake.gets
	if _, err := s.Cards(ctx); err != nil || fake.gets != gets {
		t.Fatalf("expected cached read; gets %d -> %d, err %v", gets, fake.gets, err)
	}

	// After the TTL a reload happens; if it fails the stale copy is served.
	clock = clock.Add(2 * time.Minute)
	fake.fail = true
	if db2, err := s.Cards(ctx); err != nil || db2 != db || fake.gets != gets+1 {
		t.Fatalf("expected stale fallback; err %v, gets %d", err, fake.gets)
	}
}
