package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"log"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/external"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/startup"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/subosito/gotenv"
	"github.com/wailsapp/wails/v3/pkg/application"
	"go.getarcane.app/streams/logs"
	"go.uber.org/fx"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	application.RegisterEvent[string]("time")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	app := application.New(application.Options{
		Name:        "desktop",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(&GreetService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Window 1",
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1000,
		Height: 618,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}

func Bootstrap(ctx context.Context) error {
	if err := gotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg := external.LoadConfig()
	runtimeIdentityCfg := &startup.RuntimeIdentityConfig{
		PUID:              cfg.PUID,
		PGID:              cfg.PGID,
		DockerHost:        cfg.DockerHost,
		DockerConfig:      cfg.DockerConfig,
		DatabaseURL:       cfg.DatabaseURL,
		ProjectsDirectory: cfg.ProjectsDirectory,
	}
	if err := startup.ApplyRequestedRuntimeIdentity(ctx, runtimeIdentityCfg); err != nil {
		return fmt.Errorf("apply runtime identity: %w", err)
	}
	cfg.DockerConfig = runtimeIdentityCfg.DockerConfig

	external.SetupSlogLogger(cfg)
	// Tee all slog output into the in-memory ring buffer that powers the diagnostics live log tail.
	slog.SetDefault(slog.New(logs.NewSlogHandler(slog.Default().Handler(), ws.LogBroadcaster())))
	external.SetGormLogger(external.BuildGormLogger(cfg))
	slog.InfoContext(ctx, "Arcane is starting...", "version", external.GetConfigVersion())
	startup.ApplyMemoryLimit(ctx)
	slog.InfoContext(ctx, "Arcane Identity Configuration", "puid", os.Getuid(), "pgid", os.Getgid())

	appCtx, cancelApp := context.WithCancel(ctx)
	appCtx = utils.WithAppLifecycleContext(appCtx)

	db, err := external.InitializeDBAndMigrate(appCtx, cfg)
	if err != nil {
		cancelApp()
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func() {
		cancelApp()
		if closeErr := db.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "Error closing database", "error", closeErr)
		}
	}()

	app := fx.New(external.ApplicationOptions(appCtx, cfg, db, cancelApp))

	startCtx, cancelStart := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelStart()
	if startErr := app.Start(startCtx); startErr != nil {
		return fmt.Errorf("start application: %w", startErr)
	}

	select {
	case <-ctx.Done():
		slog.InfoContext(appCtx, "Context canceled")
	case signal := <-app.Done():
		slog.InfoContext(appCtx, "Received shutdown signal", "signal", signal)
	}

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelStop()
	if stopErr := app.Stop(stopCtx); stopErr != nil {
		return fmt.Errorf("stop application: %w", stopErr)
	}

	slog.InfoContext(context.WithoutCancel(appCtx), "Arcane shutdown complete")
	return nil
}