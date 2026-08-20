package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	infraenvfile "github.com/shiyudesu/frux/internal/infra/envfile"
	infraembedding "github.com/shiyudesu/frux/internal/infra/persistence/embedding"
)

func main() {
	if err := run(); err != nil {
		log.Printf("frux multimodal provider failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	if err := infraenvfile.LoadMultimodal(infraenvfile.MultimodalTongyiAdapter); err != nil {
		return fmt.Errorf("load multimodal environment: %w", err)
	}
	config, err := infraembedding.LoadTongyiAdapterConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load adapter config: %w", err)
	}
	client, err := infraembedding.NewTongyiClient(config)
	if err != nil {
		return fmt.Errorf("initialize Tongyi client: %w", err)
	}
	adapter, err := infraembedding.NewTongyiAdapter(config, client)
	if err != nil {
		return fmt.Errorf("initialize provider adapter: %w", err)
	}
	probeContext, cancelProbe := context.WithTimeout(context.Background(), config.UpstreamTimeout)
	if err := adapter.Probe(probeContext); err != nil {
		cancelProbe()
		return fmt.Errorf("probe Tongyi model: %w", err)
	}
	cancelProbe()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr:              config.ListenAddress,
		Handler:           adapter.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      config.UpstreamTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.ListenAndServe()
	}()
	log.Printf("frux multimodal provider is listening on %s", config.ListenAddress)
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancelShutdown()
		return server.Shutdown(shutdownContext)
	}
}
