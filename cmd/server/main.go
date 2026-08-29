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

	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
	"github.com/Nickcandy/eth-fund-trace/internal/config"
	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/fundgraph"
	"github.com/Nickcandy/eth-fund-trace/internal/httpapi"
	"github.com/Nickcandy/eth-fund-trace/internal/profile"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
	"github.com/Nickcandy/eth-fund-trace/internal/syncer"
	"github.com/Nickcandy/eth-fund-trace/internal/thorchain"
	"github.com/Nickcandy/eth-fund-trace/internal/tracer"
	"github.com/Nickcandy/eth-fund-trace/internal/transactionanalysis"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/time/rate"
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
	sharedEtherscanLimiter := rate.NewLimiter(rate.Limit(cfg.EtherscanRequestsPerSecond), cfg.EtherscanBurst)
	clientConfig := etherscan.Config{
		APIKey: cfg.EtherscanAPIKey, BaseURL: cfg.EtherscanBaseURL, PageSize: cfg.EtherscanPageSize,
		MaxPages: cfg.EtherscanMaxPages, RequestsPerSecond: float64(cfg.EtherscanRequestsPerSecond),
		Burst: cfg.EtherscanBurst, MaxRetries: cfg.EtherscanMaxRetries, RetryBase: time.Duration(cfg.EtherscanRetryBaseMS) * time.Millisecond, Descending: cfg.SyncMaxRecordsPerAction > 0, HTTPClient: &http.Client{Timeout: time.Duration(cfg.EtherscanHTTPTimeoutSeconds) * time.Second}, Limiter: sharedEtherscanLimiter,
	}
	ethereumConfig := clientConfig
	ethereumConfig.Chain, ethereumConfig.ChainID = "ethereum", 1
	ethereumClient := etherscan.NewClient(ethereumConfig)
	etherscanClients := map[string]syncer.Source{"ethereum": ethereumClient}
	addressProfiler := profile.New(appStore, time.Now)
	balanceProviders := make(map[string]httpapi.NativeBalanceProvider)
	if cfg.EthereumRPCURL != "" {
		ethereumRPC := chainrpc.New(chainrpc.Config{URL: cfg.EthereumRPCURL, ChainID: 1})
		balanceProviders["ethereum"] = ethereumRPC
		if err := ethereumRPC.ValidateChain(connectCtx); err != nil {
			return err
		}
	}
	syncManager := syncer.NewMulti(etherscanClients, appStore, syncer.Config{
		CacheTTL: time.Duration(cfg.SyncCacheTTLMinutes) * time.Minute, DisableCache: cfg.SyncCacheTTLMinutes == 0, Confirmations: int64(cfg.SyncConfirmations), QueueSize: cfg.SyncQueueSize,
		MaxRecordsPerAction: cfg.SyncMaxRecordsPerAction,
		StartBlocks:         map[string]int64{"ethereum": cfg.EthereumSyncStartBlock},
		EndBlocks:           map[string]int64{"ethereum": cfg.EthereumSyncEndBlock},
		AfterAddressSynced: func(ctx context.Context, chain, address string) error {
			_, err := addressProfiler.Get(ctx, chain, address)
			return err
		},
	})
	transactionAnalyzer := transactionanalysis.New(ethereumClient, appStore, time.Now)
	crossChainVerifier := thorchain.New(thorchain.Config{
		StatusBaseURL: cfg.THORChainStatusURL, BitcoinBaseURL: cfg.BitcoinAPIURL, ClientID: cfg.THORChainClientID,
		HTTPClient: &http.Client{Timeout: time.Duration(cfg.CrossChainHTTPTimeoutSeconds) * time.Second},
	})

	e := echo.New()
	e.HideBanner = true
	httpapi.UseGovernance(e, httpapi.GovernanceConfig{APIKey: cfg.HTTPAPIKey, DisableAuth: cfg.HTTPAuthDisabled, Timeout: time.Duration(cfg.HTTPTimeoutSeconds) * time.Second, BodyLimit: cfg.HTTPBodyLimit, RequestsPerSecond: float64(cfg.HTTPRequestsPerSecond), Burst: cfg.HTTPBurst})
	e.GET("/healthz", httpapi.NewHealthHandler(client).Handle)
	syncHandler := httpapi.NewSyncHandler(syncManager)
	if !cfg.TraceExistingDataOnly {
		e.POST("/api/v1/sync", syncHandler.Enqueue)
	}
	e.GET("/api/v1/sync-jobs/latest", syncHandler.LatestJob)
	e.GET("/api/v1/sync-jobs/:id", syncHandler.Job)
	e.GET("/api/v1/addresses/:address/profile", httpapi.NewProfileHandler(addressProfiler).Get)
	e.GET("/api/v1/addresses/:address/balance", httpapi.NewBalanceHandler(balanceProviders, time.Now).Get)
	e.GET("/api/v1/addresses/:address", httpapi.NewAddressHandler(appStore).Get)
	e.GET("/api/v1/edges", httpapi.NewEdgeHandler(fundgraph.New(appStore)).Get)
	traceGraph := tracer.New(appStore).
		WithTransactionAnalyzer(transactionAnalyzer).
		WithCrossChainVerifier(crossChainVerifier).
		WithAddressInspector(transactionAnalyzer).
		WithRequiredStartBlocks(map[string]int64{"ethereum": cfg.EthereumSyncStartBlock}).
		WithExistingDataOnly(cfg.TraceExistingDataOnly)
	var traceSyncJobs tracer.SyncJobs
	if !cfg.TraceExistingDataOnly {
		traceSyncJobs = syncManager
	}
	traceManager := tracer.NewManager(traceGraph, appStore, traceSyncJobs)
	traceHandler := httpapi.NewTraceHandler(traceManager)
	e.GET("/api/v1/trace", traceHandler.Enqueue)
	e.GET("/api/v1/trace-jobs/latest", traceHandler.LatestJob)
	e.POST("/api/v1/trace-jobs/:id/stop", traceHandler.Stop)
	e.POST("/api/v1/trace-jobs/:id/extensions", traceHandler.EnqueueExtension)
	e.GET("/api/v1/trace-jobs/:id/extensions/latest", traceHandler.LatestExtension)
	e.GET("/api/v1/trace-jobs/:id", traceHandler.Job)
	e.GET("/api/v1/risk", httpapi.NewRiskHandler(traceManager).Get)
	e.GET("/api/v1/transactions/:txHash", httpapi.NewTransactionHandler(transactionAnalyzer).Get)
	labelHandler := httpapi.NewLabelHandler(appStore)
	e.POST("/api/v1/labels", labelHandler.Create)
	e.GET("/api/v1/labels", labelHandler.List)
	if err := httpapi.RegisterWeb(e, cfg.WebDistDir); err != nil {
		slog.Warn("web console not available", "directory", cfg.WebDistDir, "error", err)
	}

	serverCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !cfg.TraceExistingDataOnly {
		go func() {
			if err := syncManager.Run(serverCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("sync manager stopped", "error", err)
			}
		}()
	}
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
