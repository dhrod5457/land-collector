package collector

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
)

type Saver interface {
	SaveBatch(context.Context, []domain.Record) error
	SaveFailure(context.Context, domain.Failure) error
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

func NewRunner(fetcher Fetcher, saver Saver, endpoints []nsdi.Endpoint, workers, batchSize int) *Runner {
	return &Runner{
		fetcher:   fetcher,
		saver:     saver,
		endpoints: endpoints,
		workers:   workers,
		batchSize: batchSize,
	}
}

func (r *Runner) RunFile(ctx context.Context, path string) error {
	return r.runFile(ctx, path, "")
}

func (r *Runner) RunRegionFile(ctx context.Context, path, regionPrefix string) error {
	if regionPrefix == "" {
		return fmt.Errorf("region prefix is required")
	}
	return r.runFile(ctx, path, regionPrefix)
}

func (r *Runner) runFile(parent context.Context, path, regionPrefix string) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	jobs := make(chan string, r.workers*2)
	records := make(chan domain.Record, r.workers*max(1, len(r.endpoints)))
	producerErr := make(chan error, 1)

	go func() {
		defer close(jobs)
		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() {
			line++
			pnu := strings.TrimSpace(scanner.Text())
			if pnu == "" {
				continue
			}
			if !validPNU(pnu) {
				producerErr <- fmt.Errorf("invalid PNU at line %d: %q", line, pnu)
				cancel()
				return
			}
			if regionPrefix != "" && !strings.HasPrefix(pnu, regionPrefix) {
				continue
			}
			select {
			case jobs <- pnu:
			case <-ctx.Done():
				return
			}
		}
		producerErr <- scanner.Err()
	}()

	var workers sync.WaitGroup
	var failureMu sync.Mutex
	var failureCount int
	var firstFailure error

	recordFailure := func(pnu string, dataset domain.Dataset, fetchErr error) {
		failure := domain.Failure{
			PNU:       pnu,
			Dataset:   dataset,
			FailedAt:  time.Now().UTC(),
			ErrorText: fetchErr.Error(),
		}
		if err := r.saver.SaveFailure(ctx, failure); err != nil {
			fetchErr = fmt.Errorf("%w; persist failure: %v", fetchErr, err)
		}
		failureMu.Lock()
		failureCount++
		if firstFailure == nil {
			firstFailure = fmt.Errorf("%s/%s: %w", pnu, dataset, fetchErr)
		}
		failureMu.Unlock()
	}

	for i := 0; i < r.workers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for pnu := range jobs {
				for _, endpoint := range r.endpoints {
					record, err := r.fetcher.Fetch(ctx, endpoint, pnu)
					if err != nil {
						recordFailure(pnu, endpoint.Dataset, err)
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

	go func() {
		workers.Wait()
		close(records)
	}()

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
				cancel()
				for range records {
				}
				return err
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}
	if err := <-producerErr; err != nil {
		return err
	}

	failureMu.Lock()
	defer failureMu.Unlock()
	if failureCount > 0 {
		return fmt.Errorf("collection completed with %d failed requests; first: %w", failureCount, firstFailure)
	}
	return nil
}

func validPNU(pnu string) bool {
	if len(pnu) != 19 {
		return false
	}
	for _, ch := range pnu {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
