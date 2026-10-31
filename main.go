package main

import (
	"embed"
	"log"
	"time"

	"snap-rq/backend/commands"
	"snap-rq/backend/commands/runnables"
	"snap-rq/backend/database"
	"snap-rq/backend/services"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	// Open the local SQLite database and run migrations.
	db, err := database.Open()
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// The command framework: every operation the app can perform, invoked
	// by name from the UI (and one day an MCP server or CLI), with undo
	// for the operations that can be reversed. See specs/command-framework.md.
	engine, err := commands.NewEngine(db)
	if err != nil {
		log.Fatalf("starting the command engine: %v", err)
	}
	if err := engine.Register(runnables.Catalog...); err != nil {
		log.Fatalf("registering commands: %v", err)
	}

	settingsService := services.NewSettingsService(db)
	engine.SetUndoLimit(settingsService.UndoHistoryLimit)

	// Start validates the whole framework: the cascade manifest must match
	// the real schema, and every undo operation must resolve.
	if err := engine.Start(); err != nil {
		log.Fatalf("command framework validation: %v", err)
	}

	profileService := services.NewProfileService(db)
	projectService := services.NewProjectService(db)
	collectionService := services.NewCollectionService(db)
	environmentService := services.NewEnvironmentService(db)
	environmentVariableService := services.NewEnvironmentVariableService(db)
	favouriteService := services.NewFavouriteService(db)
	tagService := services.NewTagService(db)
	iconService := services.NewIconService(db)

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running on a macOS.
	app := application.New(application.Options{
		Name:        "snap-rq-wails-v3",
		Description: "A REST API client",
		Services: []application.Service{
			application.NewService(settingsService),
			application.NewService(profileService),
			application.NewService(projectService),
			application.NewService(collectionService),
			application.NewService(environmentService),
			application.NewService(environmentVariableService),
			application.NewService(favouriteService),
			application.NewService(tagService),
			application.NewService(iconService),
			application.NewService(commands.NewCommandService(engine)),
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
		Width:      1000,
		Height:     618,
		StartState: application.WindowStateMaximised,
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
	err = app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
