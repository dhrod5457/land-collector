package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/dhrod5457/land-collector/internal/collector"
	"github.com/dhrod5457/land-collector/internal/config"
	"github.com/dhrod5457/land-collector/internal/domain"
	"github.com/dhrod5457/land-collector/internal/provider/nsdi"
	"github.com/dhrod5457/land-collector/internal/repository/postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	repository, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer repository.Close()

	client := nsdi.NewClient(
		cfg.HTTPTimeout,
		cfg.ServiceKey,
		cfg.RequestsPerSecond,
		cfg.MaxRetries,
	)

	endpoints := []nsdi.Endpoint{
		{Dataset: domain.DatasetLand, URL: cfg.LandAPIURL},
		{Dataset: domain.DatasetCharacteristic, URL: cfg.CharacteristicURL},
		{Dataset: domain.DatasetPrice, URL: cfg.PriceURL},
		{Dataset: domain.DatasetUsePlan, URL: cfg.UsePlanURL},
	}

	runner := collector.NewRunner(
		client,
		repository,
		endpoints,
		cfg.Workers,
		cfg.BatchSize,
	)
	if err := runner.RunFile(ctx, cfg.PNUFile); err != nil {
		log.Fatal(err)
	}

	log.Println("collection completed")
}
