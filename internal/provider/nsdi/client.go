package nsdi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
)

type Endpoint struct {
	Dataset domain.Dataset
	URL     string
}

type rateGate struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
}

func newRateGate(rps int) *rateGate {
	return &rateGate{interval: time.Second / time.Duration(rps)}
}

func (g *rateGate) Wait(ctx context.Context) error {
	g.mu.Lock()
	now := time.Now()
	start := now
	if g.next.After(now) {
		start = g.next
	}
	g.next = start.Add(g.interval)
	g.mu.Unlock()

	delay := time.Until(start)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Client struct {
	httpClient *http.Client
	serviceKey string
	gate       *rateGate
	retries    int
}

func NewClient(timeout time.Duration, serviceKey string, rps, retries int) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		serviceKey: serviceKey,
		gate:       newRateGate(rps),
		retries:    retries,
	}
}

func (c *Client) Fetch(ctx context.Context, ep Endpoint, pnu string) (domain.Record, error) {
	if ep.URL == "" {
		return domain.Record{}, fmt.Errorf("endpoint for %s is empty", ep.Dataset)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if err := c.gate.Wait(ctx); err != nil {
			return domain.Record{}, err
		}
		record, retry, err := c.fetchOnce(ctx, ep, pnu)
		if err == nil {
			return record, nil
		}
		lastErr = err
		if !retry || attempt == c.retries {
			break
		}

		backoff := time.Duration(1<<attempt) * 250 * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return domain.Record{}, ctx.Err()
		case <-timer.C:
		}
	}
	return domain.Record{}, lastErr
}

func (c *Client) fetchOnce(ctx context.Context, ep Endpoint, pnu string) (domain.Record, bool, error) {
	u, err := url.Parse(ep.URL)
	if err != nil {
		return domain.Record{}, false, err
	}

	q := u.Query()
	q.Set("serviceKey", c.serviceKey)
	q.Set("pnu", pnu)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return domain.Record{}, false, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.Record{}, true, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return domain.Record{}, true, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return domain.Record{}, retry, fmt.Errorf("%s returned HTTP %d", ep.Dataset, resp.StatusCode)
	}

	return domain.Record{
		PNU:         pnu,
		Dataset:     ep.Dataset,
		ObservedAt:  time.Now().UTC(),
		PayloadJSON: body,
	}, false, nil
}
