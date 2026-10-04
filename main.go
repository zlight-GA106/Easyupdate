package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/zlight-GA106/EasyUpdate/internal/config"
	"github.com/zlight-GA106/EasyUpdate/internal/database"
)

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
	slog.Info("EasyUpdate initialized")
}
