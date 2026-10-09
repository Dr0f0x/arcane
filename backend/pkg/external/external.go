package external

import (
	"context"

	"go.uber.org/fx"
	"gorm.io/gorm/logger"

	"github.com/getarcaneapp/arcane/backend/v2/internal/bootstrap"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

func ApplicationOptions(appCtx context.Context, cfg *config.Config, db *database.DB, cancelApp context.CancelFunc) fx.Option {
	return bootstrap.ApplicationOptions(appCtx, cfg, db, cancelApp)
}

func InitializeDBAndMigrate(ctx context.Context, cfg *config.Config) (*database.DB, error) {
	return bootstrap.InitializeDBAndMigrateInternal(ctx, cfg)
}

func SetupSlogLogger(cfg* config.Config){
	bootstrap.SetupSlogLogger(cfg)
}

func BuildGormLogger(cfg* config.Config) logger.Interface{
	return bootstrap.BuildGormLogger(cfg)
}

func SetGormLogger(l logger.Interface) {
	database.SetGormLogger(l)
}

func LoadConfig() *config.Config{
	return config.Load()
}

func GetConfigVersion() string {
	return config.Version
}