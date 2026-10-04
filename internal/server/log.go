package server

import (
	"github.com/zlight-GA106/EasyUpdate/internal/database"
	"log/slog"
)

func slogApp(a database.App, id int64) {
	slog.Info("app saved", "app_id", id, "package", a.PackageName)
}
