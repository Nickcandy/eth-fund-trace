package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/config"
	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/fundgraph"
	"github.com/Nickcandy/eth-fund-trace/internal/httpapi"
	"github.com/Nickcandy/eth-fund-trace/internal/profile"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(context.Background()); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	cfg := config.Load()
	connectCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return err
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	if err := client.Ping(connectCtx, nil); err != nil {
		return err
	}
	appStore := store.New(client.Database(cfg.MongoDatabase))
	if err := appStore.Initialize(connectCtx); err != nil {
		return err
	}
	etherscanClient := etherscan.NewClient(etherscan.Config{
		APIKey: cfg.EtherscanAPIKey, BaseURL: cfg.EtherscanBaseURL, PageSize: cfg.EtherscanPageSize,
		MaxPages: cfg.EtherscanMaxPages, RequestsPerSecond: float64(cfg.EtherscanRequestsPerSecond),
		Burst: cfg.EtherscanBurst, MaxRetries: cfg.EtherscanMaxRetries, RetryBase: time.Duration(cfg.EtherscanRetryBaseMS) * time.Millisecond,
	})
	addressProfiler := profile.New(appStore, time.Now)
	syncManager := syncer.New(etherscanClient, appStore, syncer.Config{
		CacheTTL: time.Duration(cfg.SyncCacheTTLMinutes) * time.Minute, Confirmations: int64(cfg.SyncConfirmations), QueueSize: cfg.SyncQueueSize,
		AfterAddressSynced: func(ctx context.Context, chain, address string) error {
			_, err := addressProfiler.Get(ctx, chain, address)
			return err
		},
	})

	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", httpapi.NewHealthHandler(client).Handle)
	syncHandler := httpapi.NewSyncHandler(syncManager)
	e.POST("/api/v1/sync", syncHandler.Enqueue)
	e.GET("/api/v1/sync-jobs/:id", syncHandler.Job)
	e.GET("/api/v1/addresses/:address/profile", httpapi.NewProfileHandler(addressProfiler).Get)
	e.GET("/api/v1/edges", httpapi.NewEdgeHandler(fundgraph.New(appStore)).Get)
	traceManager := tracer.NewManager(tracer.New(appStore), appStore, syncManager)
	traceHandler := httpapi.NewTraceHandler(traceManager)
	e.GET("/api/v1/trace", traceHandler.Enqueue)
	e.GET("/api/v1/trace-jobs/:id", traceHandler.Job)

	serverCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := syncManager.Run(serverCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("sync manager stopped", "error", err)
		}
	}()
	go func() {
		if err := traceManager.Run(serverCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("trace manager stopped", "error", err)
		}
	}()
	go func() {
		<-serverCtx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = e.Shutdown(shutdownCtx)
	}()
	if err := e.Start(cfg.HTTPAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
