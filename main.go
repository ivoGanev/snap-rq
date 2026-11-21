package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"snap-rq/backend/backup"
	"snap-rq/backend/commands"
	"snap-rq/backend/commands/backups"
	"snap-rq/backend/commands/catalog"
	"snap-rq/backend/commands/requests"
	"snap-rq/backend/database"
	"snap-rq/backend/mcp"
	"snap-rq/backend/secrets"
	"snap-rq/backend/selfupdate"

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
// `-ldflags "-X main.currentVersion=<version>"`. Local builds keep this valid
// SemVer default so the updater initialises, and treats them as older than any release.
var currentVersion = "0.0.0-dev"

// updaterRepository is the GitHub repository (owner/repo) that publishes
// releases for the self-updater.
const updaterRepository = "ivoGanev/snap-rq"

func init() {
	application.RegisterEvent[string]("time")
}

func main() {
	// Secret variable values are encrypted with a key kept in the OS
	// credential store (a 0600 file when there is none). A moved data folder
	// (the e2e tests' throwaway one) keeps its own key file and never touches
	// the credential store, so it can't overwrite the real profile's key.
	keyFile := filepath.Join(database.DataDir(), "secret.key")
	keyStore := secrets.OSKeyStore(keyFile)
	if database.CustomDataDir() {
		keyStore = secrets.FileKeyStore(keyFile)
	}
	secrets.SetDefault(secrets.NewVault(keyStore))

	// Open the local SQLite database and run migrations. After an update a
	// backup is taken first, since migrations can reshape tables.
	backup.AppVersion = currentVersion
	// A restore prepared before the last restart is swapped in before the
	// database is opened.
	if name, err := backup.ApplyPendingRestore(database.DataDir(), database.Path()); err != nil {
		log.Printf("restore: %v", err)
	} else if name != "" {
		log.Printf("restored backup %s", name)
	}
	db, err := database.OpenWith(database.Path(), backup.BeforeUpdate(currentVersion))
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
	for _, f := range []commands.ObservableFactory{requests.NewSend, backups.NewCreate, backups.NewRestore} {
		if err := runner.RegisterObservable(f); err != nil {
			log.Fatalf("registering observable commands: %v", err)
		}
	}
	// Risky commands (big deletes, imports, anything that can't be undone)
	// take a whole-database backup first; the window shows a toast once the
	// file is written.
	runner.SetBackupGuard(backup.Guard(db,
		func(info backup.Info) {
			if app := application.Get(); app != nil {
				app.Event.Emit("backup:saved", map[string]string{"reason": info.Header.Reason, "path": info.Path})
			}
		},
		func(reason string, err error) {
			log.Printf("backup %s failed: %v", reason, err)
			if app := application.Get(); app != nil {
				app.Event.Emit("backup:failed", map[string]string{"reason": reason, "error": err.Error()})
			}
		},
	))
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

	// MCP server: opt-in, loopback only, a curated allowlist of commands
	// run through the same runner as the UI. See docs/mcp.md.
	mcpStore := mcp.NewStore(db)
	mcpServer := mcp.NewServer(runner, mcpStore, currentVersion)
	mcpServer.StartIfEnabled()
	defer mcpServer.Stop()

	updaterService := &UpdaterService{}
	appService := &AppService{}

	app := application.New(application.Options{
		Name:        "snap-rq-wails-v3",
		Description: "A REST API client",
		Services: []application.Service{
			application.NewService(commands.NewCommandService(runner)),
			application.NewService(updaterService),
			application.NewService(appService),
			application.NewService(mcp.NewSettingsService(mcpServer, mcpStore)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	updaterService.app = app
	appService.app = app
	// win is set once the window exists. An approval request flashes its
	// taskbar button until the user comes back, and restores it if minimised;
	// it does not take focus, so it never interrupts typing elsewhere.
	var win *application.WebviewWindow
	mcpServer.SetEmitter(func(name string, data any) {
		app.Event.Emit(name, data)
		if win == nil {
			return
		}
		switch name {
		case "mcp:approval":
			if win.IsMinimised() {
				win.UnMinimise()
			}
			win.Flash(true)
		case "mcp:approval-closed":
			win.Flash(false)
		}
	})

	if err := configureUpdater(app); err != nil {
		log.Fatalf("configuring updater: %v", err)
	}

	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
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

// AppService exposes app-level actions to the frontend.
type AppService struct {
	app *application.App
}

// Restart starts a new SnapRQ and quits this one, e.g. to finish restoring a
// backup (the new process swaps the database in before opening it).
func (s *AppService) Restart() error {
	if s.app == nil {
		return errors.New("the app is not ready yet")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := exec.Command(exe, os.Args[1:]...).Start(); err != nil {
		return fmt.Errorf("starting SnapRQ again: %w", err)
	}
	s.app.Quit()
	return nil
}

// ShowInFolder opens the system file manager at a folder, or at a file's
// folder with the file selected (e.g. a backup).
func (s *AppService) ShowInFolder(path string) error {
	if s.app == nil {
		return errors.New("the app is not ready yet")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return s.app.Env.OpenFileManager(path, !info.IsDir())
}

// UpdaterService exposes the self-updater to the frontend. The native
// application menu is deliberately avoided because its Win32 menu bar causes a
// WebView2 mouse-input bug on Windows (a dismissed dropdown leaves the web UI
// stuck with the left button held).
type UpdaterService struct {
	app *application.App
}

// UpdateInfo describes a release that can be installed.
type UpdateInfo struct {
	Version string `json:"version"`
}

// CurrentVersion is the version this build reports to the updater.
func (s *UpdaterService) CurrentVersion() string { return currentVersion }

// Check asks GitHub Releases for a newer version. It returns nil when SnapRQ
// is up to date. Nothing is downloaded until Install is called.
func (s *UpdaterService) Check() (*UpdateInfo, error) {
	if s.app == nil {
		return nil, errors.New("the app is not ready yet")
	}
	release, err := s.app.Updater.Check(context.Background())
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	return &UpdateInfo{Version: release.Version}, nil
}

// Install downloads, verifies and stages the release that Check found. The
// settings window shows progress from the updater's events. Restart applies it.
func (s *UpdaterService) Install() error {
	if s.app == nil {
		return errors.New("the app is not ready yet")
	}
	return s.app.Updater.DownloadAndInstall(context.Background())
}

// Restart relaunches SnapRQ to finish the staged update.
func (s *UpdaterService) Restart() error {
	if s.app == nil {
		return errors.New("the app is not ready yet")
	}
	return s.app.Updater.Restart(context.Background())
}

// configureUpdater wires the in-app self-updater to GitHub Releases.
func configureUpdater(app *application.App) error {
	gh, err := github.New(github.Config{
		Repository:    updaterRepository,
		ChecksumAsset: "SHA256SUMS",
		AssetMatcher:  matchUpdateAsset,
	})
	if err != nil {
		return err
	}
	// Headless: the settings window shows the update flow itself.
	return app.Updater.Init(updater.Config{
		CurrentVersion: currentVersion,
		Providers:      []updater.Provider{gh},
		Window:         updater.WindowNone,
	})
}

// matchUpdateAsset picks the release asset the updater installs: the raw
// executable (or macOS .zip), never an installer or distro package.
func matchUpdateAsset(_ updater.CheckRequest, assets []github.ReleaseAsset) int {
	for i, a := range assets {
		if selfupdate.IsUpdateAsset(a.Name, runtime.GOOS, runtime.GOARCH) {
			return i
		}
	}
	return -1
}
