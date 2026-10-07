package postgres

import (
	"context"
	"fmt"

	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Repository, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) SaveBatch(ctx context.Context, records []domain.Record) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	for _, record := range records {
		_, err = tx.Exec(ctx, `
			INSERT INTO land_records (pnu, dataset, observed_at, payload)
			VALUES ($1, $2, $3, $4::jsonb)
			ON CONFLICT (pnu, dataset)
			DO UPDATE SET observed_at = EXCLUDED.observed_at, payload = EXCLUDED.payload
		`, record.PNU, string(record.Dataset), record.ObservedAt, record.PayloadJSON)
		if err != nil {
			return fmt.Errorf("save %s/%s: %w", record.PNU, record.Dataset, err)
		}

		_, err = tx.Exec(ctx, `
			DELETE FROM failed_requests
			WHERE pnu = $1 AND dataset = $2
		`, record.PNU, string(record.Dataset))
		if err != nil {
			return fmt.Errorf("clear failure %s/%s: %w", record.PNU, record.Dataset, err)
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) SaveFailure(ctx context.Context, failure domain.Failure) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO failed_requests (pnu, dataset, failed_at, error_text, attempts)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (pnu, dataset)
		DO UPDATE SET
			failed_at = EXCLUDED.failed_at,
			error_text = EXCLUDED.error_text,
			attempts = failed_requests.attempts + 1
	`, failure.PNU, string(failure.Dataset), failure.FailedAt, failure.ErrorText)
	if err != nil {
		return fmt.Errorf("save failure %s/%s: %w", failure.PNU, failure.Dataset, err)
	}
	return nil
}
