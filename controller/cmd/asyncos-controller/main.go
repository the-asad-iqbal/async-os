// Command asyncos-controller runs the AsyncOS PV, genset and export-limit
// controller: device polling, the control loop, statistics and the web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/the-asad-iqbal/async-os/controller/internal/api"
	"github.com/the-asad-iqbal/async-os/controller/internal/audit"
	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
	"github.com/the-asad-iqbal/async-os/controller/internal/devices"
	"github.com/the-asad-iqbal/async-os/controller/internal/energy"
	"github.com/the-asad-iqbal/async-os/controller/internal/plant"
	"github.com/the-asad-iqbal/async-os/controller/internal/sim"
	"github.com/the-asad-iqbal/async-os/controller/internal/stats"
	"github.com/the-asad-iqbal/async-os/controller/internal/store"
	"github.com/the-asad-iqbal/async-os/controller/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

var siteKey = []byte("site")

func main() {
	listen := flag.String("listen", ":80", "HTTP listen address")
	dataDir := flag.String("data", "/var/lib/asyncos", "directory for the database and setup code")
	profilesDir := flag.String("profiles", "", "extra device profiles (default <data>/profiles)")
	demo := flag.String("demo", "", `run against a simulated site: "grid", "hybrid" or "ats"`)
	localDisplay := flag.Bool("local-display", false, "give the device's own kiosk screen (loopback, no session) read-only access")
	rebootRequest := flag.String("reboot-request", "", "request reboots by creating this file (watched by a privileged unit) instead of calling systemctl")
	debug := flag.Bool("debug", false, "verbose logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	reboot := func() error { return exec.Command("systemctl", "reboot").Run() }
	if *rebootRequest != "" {
		path := *rebootRequest
		reboot = func() error { return os.WriteFile(path, []byte("reboot\n"), 0o644) }
	}
	if err := run(log, *listen, *dataDir, *profilesDir, *demo, *localDisplay, reboot); err != nil {
		log.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, listen, dataDir, profilesDir, demo string, localDisplay bool, reboot func() error) error {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	if profilesDir == "" {
		profilesDir = filepath.Join(dataDir, "profiles")
	}
	db, err := store.Open(filepath.Join(dataDir, "asyncos.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	lib, err := devices.LoadLibrary(profilesDir)
	if err != nil {
		return fmt.Errorf("device library: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	site, err := loadSite(db, lib, log)
	if err != nil {
		return err
	}
	if demo != "" {
		simSite, err := sim.Start("127.0.0.1:0", sim.Scenario(demo))
		if err != nil {
			return fmt.Errorf("simulator: %w", err)
		}
		defer simSite.Close()
		go simSite.Run(ctx)
		site = simSite.Config()
		log.Warn("demo mode: controlling a simulated site", "scenario", demo, "modbus", simSite.Address)
	}

	authService := auth.NewService(db, filepath.Join(dataDir, "setup-code"))
	if err := authService.EnsureSetupCode(); err != nil {
		return err
	}
	if needed, _ := authService.NeedsSetup(); needed {
		log.Warn("no users yet: finish setup in the web UI with the code shown on the device console", "code_file", filepath.Join(dataDir, "setup-code"))
	}

	engine, err := plant.NewEngine(lib, site, log)
	if err != nil {
		return fmt.Errorf("site configuration: %w", err)
	}
	// Grid/genset meter totals survive restarts.
	totalsKey := []byte("source_totals")
	engine.LoadTotals = func(id string) energy.Totals {
		saved := map[string]energy.Totals{}
		_ = db.View(func(tx *bolt.Tx) error { return store.GetJSON(tx, store.BucketMeta, totalsKey, &saved) })
		return saved[id]
	}
	engine.SaveTotals = func(totals map[string]energy.Totals) {
		err := db.Update(func(tx *bolt.Tx) error {
			saved := map[string]energy.Totals{}
			_ = store.GetJSON(tx, store.BucketMeta, totalsKey, &saved)
			for id, t := range totals {
				saved[id] = t
			}
			return store.PutJSON(tx, store.BucketMeta, totalsKey, saved)
		})
		if err != nil {
			log.Error("saving grid/genset totals failed", "error", err)
		}
	}
	recorder := stats.NewRecorder(db, log)
	engine.OnSnapshot = recorder.Observe
	defer recorder.Flush()

	server := api.New(api.Deps{
		Auth:    authService,
		Audit:   audit.New(db, log, 100_000),
		Engine:  engine,
		Stats:   recorder,
		Library: lib,
		SaveSite: func(s plant.Site) error {
			return db.Update(func(tx *bolt.Tx) error { return store.PutJSON(tx, store.BucketMeta, siteKey, s) })
		},
		Reboot:       func() error { return exec.Command("systemctl", "reboot").Run() },
		Web:          web.FS(),
		Version:      version,
		Log:          log,
		LocalDisplay: localDisplay,
	})

	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		engine.Run(ctx)
	}()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := authService.PruneSessions(); err != nil {
					log.Warn("session cleanup failed", "error", err)
				}
			}
		}
	}()

	httpServer := &http.Server{
		Addr:              listen,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: the live view is a long-lived event stream.
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Info("AsyncOS controller started", "version", version, "listen", listen)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			<-engineDone
			return err
		}
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	<-engineDone
	return nil
}

// loadSite reads the saved configuration, or starts with an empty site.
func loadSite(db *store.Store, lib *devices.Library, log *slog.Logger) (plant.Site, error) {
	site := plant.DefaultSite()
	err := db.View(func(tx *bolt.Tx) error { return store.GetJSON(tx, store.BucketMeta, siteKey, &site) })
	switch {
	case errors.Is(err, store.ErrNotFound):
		return plant.DefaultSite(), nil
	case err != nil:
		return site, fmt.Errorf("read site configuration: %w", err)
	}
	if err := site.Validate(lib); err != nil {
		// A profile removed from the library must not brick the
		// controller: start empty and say why.
		log.Error("saved site configuration is invalid, starting in monitor mode with no devices", "error", err)
		empty := plant.DefaultSite()
		empty.Name, empty.Timezone = site.Name, site.Timezone
		if empty.Validate(lib) != nil {
			return plant.DefaultSite(), nil
		}
		return empty, nil
	}
	return site, nil
}
