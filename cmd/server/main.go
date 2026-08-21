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
	"github.com/Nickcandy/eth-fund-trace/internal/httpapi"
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

	e := echo.New()
	e.HideBanner = true
	e.GET("/healthz", httpapi.NewHealthHandler(client).Handle)

	serverCtx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
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
