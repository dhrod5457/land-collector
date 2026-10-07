package collector

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
)

type fakeFetcher struct{}

func (fakeFetcher) Fetch(_ context.Context, ep nsdi.Endpoint, pnu string) (domain.Record, error) {
	return domain.Record{
		PNU:         pnu,
		Dataset:     ep.Dataset,
		PayloadJSON: []byte(`{"ok":true}`),
	}, nil
}

type fakeSaver struct {
	mu      sync.Mutex
	records []domain.Record
}

func (s *fakeSaver) SaveBatch(_ context.Context, records []domain.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, records...)
	return nil
}

func TestRunFileCollectsAllDatasets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnu.txt")
	if err := os.WriteFile(
		path,
		[]byte("1111010100100010000\n1111010100100020000\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	saver := &fakeSaver{}
	endpoints := []nsdi.Endpoint{
		{Dataset: domain.DatasetLand, URL: "http://example.test"},
		{Dataset: domain.DatasetPrice, URL: "http://example.test"},
	}
	runner := NewRunner(fakeFetcher{}, saver, endpoints, 2, 2)
	if err := runner.RunFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(saver.records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(saver.records))
	}
}

func TestRunFileRejectsInvalidPNU(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnu.txt")
	if err := os.WriteFile(path, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(fakeFetcher{}, &fakeSaver{}, nil, 1, 1)
	if err := runner.RunFile(context.Background(), path); err == nil {
		t.Fatal("expected invalid PNU error")
	}
}
