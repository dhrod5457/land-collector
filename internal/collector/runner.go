package collector

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
)

type Saver interface {
	SaveBatch(context.Context, []domain.Record) error
}

type Fetcher interface {
	Fetch(context.Context, nsdi.Endpoint, string) (domain.Record, error)
}

type Runner struct {
	fetcher   Fetcher
	saver     Saver
	endpoints []nsdi.Endpoint
	workers   int
	batchSize int
}

func NewRunner(
	fetcher Fetcher,
	saver Saver,
	endpoints []nsdi.Endpoint,
	workers int,
	batchSize int,
) *Runner {
	return &Runner{
		fetcher:   fetcher,
		saver:     saver,
		endpoints: endpoints,
		workers:   workers,
		batchSize: batchSize,
	}
}

func (r *Runner) RunFile(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	jobs := make(chan string, r.workers*2)
	records := make(chan domain.Record, r.workers*len(r.endpoints))
	errCh := make(chan error, r.workers+1)

	var workers sync.WaitGroup
	for i := 0; i < r.workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for pnu := range jobs {
				for _, endpoint := range r.endpoints {
					record, err := r.fetcher.Fetch(ctx, endpoint, pnu)
					if err != nil {
						select {
						case errCh <- fmt.Errorf("%s/%s: %w", pnu, endpoint.Dataset, err):
						default:
						}
						continue
					}
					select {
					case records <- record:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	writerDone := make(chan error, 1)
	go func() {
		batch := make([]domain.Record, 0, r.batchSize)

		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			if err := r.saver.SaveBatch(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0]
			return nil
		}

		for record := range records {
			batch = append(batch, record)
			if len(batch) >= r.batchSize {
				if err := flush(); err != nil {
					writerDone <- err
					return
				}
			}
		}
		writerDone <- flush()
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		pnu := scanner.Text()
		if pnu == "" {
			continue
		}
		select {
		case jobs <- pnu:
		case <-ctx.Done():
			close(jobs)
			return ctx.Err()
		}
	}
	close(jobs)

	if err := scanner.Err(); err != nil {
		return err
	}

	workers.Wait()
	close(records)

	if err := <-writerDone; err != nil {
		return err
	}

	close(errCh)
	var failures int
	for range errCh {
		failures++
	}
	if failures > 0 {
		return fmt.Errorf("collection completed with %d failed requests", failures)
	}
	return nil
}
