package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"tinydm/internal/api"
	"tinydm/internal/audit"
	"tinydm/internal/auth"
	"tinydm/internal/cluster"
	"tinydm/internal/config"
	"tinydm/internal/db"
	"tinydm/internal/repo"
	"tinydm/internal/storage"
	"tinydm/internal/web"
)

// Build metadata — overridden at link time via -ldflags.
// go build -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234 -X main.buildDate=2026-01-01T00:00:00Z"
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

const banner = `
 _____  _                   ____   __  __
|_   _|(_)  _ __    _   _  |  _ \ |  \/  |
  | |  | | | '_ \  | | | | | | | || |\/| |
  | |  | | | | | | | |_| | | |_| || |  | |
  |_|  |_| |_| |_|  \__, | |____/ |_|  |_|
                     |___/
  Simple Document Management  v%s
`

func main() {
	fmt.Printf(banner, version)

	// ── Logger ────────────────────────────────────────────────────────────────
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)
	slog.Info("starting TinyDM", "version", version, "commit", commit, "built", buildDate)

	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	slog.Info("node identity", "node_id", cfg.NodeID)

	// ── Database ──────────────────────────────────────────────────────────────
	database, err := db.Open(cfg.DBDriver, cfg.DSN())
	if err != nil {
		slog.Error("failed to open database", "error", err, "driver", cfg.DBDriver)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("database ready", "driver", cfg.DBDriver, "dsn", cfg.DSN())

	// ── Storage ───────────────────────────────────────────────────────────────
	fileStore, err := storage.New(storage.BackendConfig{
		Backend:            cfg.StorageBackend,
		Path:               cfg.StoragePath,
		S3Bucket:           cfg.S3Bucket,
		S3Region:           cfg.S3Region,
		S3Endpoint:         cfg.S3Endpoint,
		S3KeyID:            cfg.S3KeyID,
		S3Secret:           cfg.S3Secret,
		AzureAccount:       cfg.AzureAccount,
		AzureKey:           cfg.AzureKey,
		AzureContainer:     cfg.AzureContainer,
		AzureEndpoint:      cfg.AzureEndpoint,
		GCSBucket:          cfg.GCSBucket,
		GCSProject:         cfg.GCSProject,
		GCSCredentialsFile: cfg.GCSCredentialsFile,
	})
	if err != nil {
		slog.Error("failed to initialise storage", "error", err, "backend", cfg.StorageBackend)
		os.Exit(1)
	}
	slog.Info("storage ready", "backend", cfg.StorageBackend)

	// ── Cluster coordination ──────────────────────────────────────────────────
	// For PostgreSQL multi-node deployments: use advisory-lock-based locker and
	// leader elector. For SQLite (single-node): use no-op implementations that
	// return immediately without touching the database.
	var locker cluster.Locker
	var leader cluster.LeaderElector

	if cfg.DBDriver == "postgres" {
		locker = cluster.NewPGLocker(database.DB)
		leader = cluster.NewPGLeaderElector(cfg.NodeID, database.DB)
	} else {
		locker = cluster.NewNoOpLocker()
		leader = cluster.NewNoOpLeaderElector(cfg.NodeID)
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := leader.Start(startCtx); err != nil {
		slog.Error("failed to start leader elector", "error", err)
		startCancel()
		os.Exit(1)
	}
	startCancel()
	slog.Info("cluster coordination ready", "node_id", leader.NodeID(), "is_leader", leader.IsLeader())

	// ── Stores ────────────────────────────────────────────────────────────────
	authStore := auth.NewStore(database)
	repoStore := repo.NewStore(database)
	auditStore := audit.NewStore(database)

	// Bootstrap: seed the first admin if the DB is empty and a password is set.
	if cfg.BootstrapAdminPass != "" {
		if err := authStore.EnsureAdminUser(
			context.Background(),
			cfg.BootstrapAdminUser,
			cfg.BootstrapAdminEmail,
			cfg.BootstrapAdminPass,
		); err != nil {
			slog.Error("bootstrap failed", "error", err)
			os.Exit(1)
		}
		slog.Info("bootstrap complete (no-op if users already exist)",
			"user", cfg.BootstrapAdminUser,
		)
	}

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(api.SecurityHeaders)
	r.Use(auth.Authenticator(cfg.JWTSecret, authStore))

	// Health endpoint — probes DB and storage, includes node identity.
	r.Get("/health", handleHealth(database, fileStore, leader))

	// Register all API routes
	api.RegisterRoutes(r, cfg, repoStore, authStore, fileStore, auditStore, locker)

	// Register admin web UI routes
	webHandler := web.New(cfg, repoStore, authStore, auditStore, fileStore)
	web.RegisterRoutes(r, webHandler)

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received", "node_id", leader.NodeID())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	leader.Stop()
	slog.Info("server stopped")
}

// handleHealth returns a liveness/readiness handler that probes the database
// and storage backend and includes the node identity in the response.
func handleHealth(database interface{ PingContext(context.Context) error }, store storage.Store, leader cluster.LeaderElector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		overallOK := true

		dbStatus := "ok"
		if err := database.PingContext(ctx); err != nil {
			slog.Error("health check: db ping failed", "error", err)
			dbStatus = "error"
			overallOK = false
		}

		storageStatus := "ok"
		if err := store.Ping(ctx); err != nil {
			slog.Error("health check: storage ping failed", "error", err)
			storageStatus = "error"
			overallOK = false
		}

		status := "ok"
		code := http.StatusOK
		if !overallOK {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		fmt.Fprintf(w,
			`{"status":%q,"version":%q,"commit":%q,"node_id":%q,"db":%q,"storage":%q}`,
			status, version, commit, leader.NodeID(), dbStatus, storageStatus,
		)
	}
}
