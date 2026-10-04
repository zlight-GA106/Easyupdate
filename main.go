package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zlight-GA106/EasyUpdate/internal/config"
	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"github.com/zlight-GA106/EasyUpdate/internal/server"
)

//go:embed templates static
var assets embed.FS

func main() {
	path := flag.String("config", "config.yaml", "configuration file")
	flag.Parse()
	c, err := config.Load(*path)
	if err != nil {
		slog.Error("configuration", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(c.Database.Path)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer db.DB.Close()
	if err = os.MkdirAll(c.Storage.Path, 0700); err != nil {
		slog.Error("storage", "error", err)
		os.Exit(1)
	}
	handler, err := server.New(c, db, assets)
	c.Admin.Password = ""
	if err != nil {
		slog.Error("server initialization", "error", err)
		os.Exit(1)
	}
	httpServer := &http.Server{Addr: c.Server.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 10 * time.Minute, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown", "error", err)
		}
	}()
	slog.Info("server start", "listen", c.Server.Listen, "public_url", c.Server.PublicURL)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
}
