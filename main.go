package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/database"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

// currentVersion is the version of the running binary, compared against the
// GitHub release tag by the updater. It is injected at build time via
// `-ldflags "-X main.currentVersion=<version>"`; "dev" is the local default.
var currentVersion = "dev"

// updaterRepository is the GitHub repository (owner/repo) that publishes
// releases for the self-updater.
const updaterRepository = "ivoGanev/snap-rq"

func init() {
	application.RegisterEvent[string]("time")
}

func main() {
	// Open the local SQLite database and run migrations.
	db, err := database.Open()
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// The command framework: every operation the app can perform, invoked
	// by name, with undo for the operations that can be reversed. See
	// specs/command-framework.md.
	runner := commands.NewRunner(db)
	if err := runner.Register(catalog.All...); err != nil {
		log.Fatalf("registering commands: %v", err)
	}
	if err := runner.RegisterObservable(requests.NewSend); err != nil {
		log.Fatalf("registering observable commands: %v", err)
	}
	runner.SetUndoLimit(func() int {
		var limit int
		var raw string
		if err := db.QueryRow(
			`SELECT value FROM app_settings WHERE key = 'undo_history_limit'`).Scan(&raw); err == nil {
			_ = jsonUnmarshal(raw, &limit)
		}
		if limit < 10 || limit > 1000 {
			limit = 100
		}
		return limit
	})

	app := application.New(application.Options{
		Name:        "snap-rq-wails-v3",
		Description: "A REST API client",
		Services: []application.Service{
			application.NewService(commands.NewCommandService(runner)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	if err := configureUpdater(app); err != nil {
		log.Fatalf("configuring updater: %v", err)
	}

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Window 1",
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:      1000,
		Height:     618,
		StartState: application.WindowStateMaximised,
		// Show the application menu (App → Check for Updates…) in the window.
		UseApplicationMenu: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
func jsonUnmarshal(raw string, dest any) error {
	return json.Unmarshal([]byte(raw), dest)
}

// configureUpdater wires the in-app self-updater to GitHub Releases and adds a
// "Check for Updates…" item to the application menu.
func configureUpdater(app *application.App) error {
	gh, err := github.New(github.Config{
		Repository:    updaterRepository,
		ChecksumAsset: "SHA256SUMS",
	})
	if err != nil {
		return err
	}
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: currentVersion,
		Providers:      []updater.Provider{gh},
	}); err != nil {
		return err
	}

	menu := app.NewMenu()
	appMenu := menu.AddSubmenu("App")
	appMenu.Add("Check for Updates…").OnClick(func(*application.Context) {
		go func() {
			if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
				log.Printf("update check failed: %v", err)
			}
		}()
	})
	app.Menu.SetApplicationMenu(menu)

	return nil
}
