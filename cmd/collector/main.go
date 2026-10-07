package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhrod5457/land-collector/internal/config"
	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
	sqliterepo "github.com/dhrod5457/land-collector/internal/repository/sqlite"
	"github.com/dhrod5457/land-collector/internal/scheduler"
	"github.com/dhrod5457/land-collector/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	repository, err := sqliterepo.New(ctx, cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()

	client := nsdi.NewClient(
		cfg.HTTPTimeout,
		cfg.ServiceKey,
		map[domain.Dataset]int{
			domain.DatasetLand:           cfg.LandRPS,
			domain.DatasetCharacteristic: cfg.CharacteristicRPS,
			domain.DatasetPrice:          cfg.PriceRPS,
			domain.DatasetUsePlan:        cfg.UsePlanRPS,
		},
		cfg.MaxRetries,
	)

	endpoints := []nsdi.Endpoint{
		{Dataset: domain.DatasetLand, URL: cfg.LandAPIURL},
		{Dataset: domain.DatasetCharacteristic, URL: cfg.CharacteristicURL},
		{Dataset: domain.DatasetPrice, URL: cfg.PriceURL},
		{Dataset: domain.DatasetUsePlan, URL: cfg.UsePlanURL},
	}

	kst := time.FixedZone("KST", 9*60*60)
	manager := scheduler.New(
		ctx,
		repository,
		client,
		repository,
		endpoints,
		cfg.Workers,
		cfg.BatchSize,
		cfg.PNUFile,
		kst,
	)
	go manager.Start(ctx)

	admin, err := web.NewServer(repository, manager, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              cfg.AdminAddr,
		Handler:           admin.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("admin server listening on %s", cfg.AdminAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("admin server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("admin shutdown: %v", err)
	}
}
