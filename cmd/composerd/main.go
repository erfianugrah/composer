package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"

	composer "github.com/erfianugrah/composer"
	"github.com/erfianugrah/composer/internal/api"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/infra/cache"
	"github.com/erfianugrah/composer/internal/infra/crypto"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
	infraGit "github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/notify"
	sopsInfra "github.com/erfianugrah/composer/internal/infra/sops"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// Config holds all configuration, resolved from environment variables with defaults.
type Config struct {
	Port         int
	DBUrl        string
	ValkeyURL    string
	StacksDir    string
	DataDir      string
	DockerHost   string
	LogLevel     string
	LogFormat    string
	NotifyURL    string
	SlackWebhook string
}

// loadConfig reads configuration from COMPOSER_* environment variables with defaults.
func loadConfig() Config {
	return Config{
		Port:         envInt("COMPOSER_PORT", 8080),
		DBUrl:        envStr("COMPOSER_DB_URL", ""),
		ValkeyURL:    envStr("COMPOSER_VALKEY_URL", ""),
		StacksDir:    envStr("COMPOSER_STACKS_DIR", "/opt/stacks"),
		DataDir:      envStr("COMPOSER_DATA_DIR", "/opt/composer"),
		DockerHost:   envStr("COMPOSER_DOCKER_HOST", ""),
		LogLevel:     envStr("COMPOSER_LOG_LEVEL", "info"),
		LogFormat:    envStr("COMPOSER_LOG_FORMAT", "json"),
		NotifyURL:    envStr("COMPOSER_NOTIFY_URL", ""),
		SlackWebhook: envStr("COMPOSER_SLACK_WEBHOOK", ""),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func buildLogger(cfg Config) (*zap.Logger, error) {
	var zapCfg zap.Config
	if cfg.LogFormat == "json" {
		zapCfg = zap.NewProductionConfig()
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}

	switch cfg.LogLevel {
	case "debug":
		zapCfg.Level.SetLevel(zap.DebugLevel)
	case "warn":
		zapCfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		zapCfg.Level.SetLevel(zap.ErrorLevel)
	default:
		zapCfg.Level.SetLevel(zap.InfoLevel)
	}

	return zapCfg.Build()
}

func main() {
	cfg := loadConfig()

	logger, err := buildLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("composerd starting",
		zap.Int("port", cfg.Port),
		zap.String("stacks_dir", cfg.StacksDir),
		zap.String("log_level", cfg.LogLevel),
	)

	ctx := context.Background()

	// --- Database (Postgres or SQLite) ---
	db, err := store.New(ctx, cfg.DBUrl, cfg.DataDir)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer db.Close()
	logger.Info("database connected, migrations applied", zap.String("type", string(db.Type)))

	// --- Repositories ---
	userRepo := store.NewUserRepo(db.SQL)
	sessionRepo := store.NewSessionRepo(db.SQL)
	apiKeyRepo := store.NewAPIKeyRepo(db.SQL)
	stackRepo := store.NewStackRepo(db.SQL)
	gitConfigRepo := store.NewGitConfigRepo(db.SQL)

	// --- Docker (optional -- graceful degradation) ---
	var dockerClient *docker.Client
	var compose *docker.Compose
	var stackSvc *app.StackService

	dockerClient, err = docker.NewClient(cfg.DockerHost)
	if err != nil {
		logger.Warn("docker not available, stack management disabled", zap.Error(err))
	} else {
		logger.Info("docker connected",
			zap.String("runtime", dockerClient.Runtime()),
			zap.String("host", dockerClient.Host()),
		)
		compose = docker.NewCompose(dockerClient.Host(), logger)
	}

	// --- Valkey Cache (optional) ---
	var valkeyCache *cache.Valkey
	if cfg.ValkeyURL != "" {
		valkeyCache, err = cache.New(ctx, cfg.ValkeyURL)
		if err != nil {
			logger.Warn("valkey not available, caching disabled", zap.Error(err))
		} else if valkeyCache != nil {
			defer valkeyCache.Close()
			logger.Info("valkey connected")
		}
	}
	// Valkey cache wiring happens after authSvc is created (see below)

	// --- Event Bus ---
	bus := eventbus.NewMemoryBus(256)

	// --- Notifications (optional) ---
	var notifyConfigs []notify.Config
	if cfg.NotifyURL != "" {
		notifyConfigs = append(notifyConfigs, notify.Config{Type: "webhook", URL: cfg.NotifyURL, Enabled: true})
		logger.Info("notifications enabled", zap.String("type", "webhook"), zap.String("url", cfg.NotifyURL))
	}
	if cfg.SlackWebhook != "" {
		notifyConfigs = append(notifyConfigs, notify.Config{Type: "slack", URL: cfg.SlackWebhook, Enabled: true})
		logger.Info("notifications enabled", zap.String("type", "slack"))
	}
	if len(notifyConfigs) > 0 {
		notifier := notify.NewNotifier(notifyConfigs, logger)
		notifier.Subscribe(bus)
	}

	// --- Stacks directory ---
	if err := os.MkdirAll(cfg.StacksDir, 0755); err != nil {
		logger.Fatal("failed to create stacks directory", zap.Error(err))
	}

	// --- Encrypt SSH keys at rest ---
	//
	// SAFETY: In production (the official Docker image) composerd runs as the
	// `composer` user with HOME=/home/composer, and the SSH key dir is
	// /home/composer/.ssh by design. Running composerd ad-hoc on a developer
	// machine previously encrypted the developer's personal ~/.ssh — a
	// destructive side effect if the auto-generated encryption key lived in a
	// throwaway data dir.
	//
	// New policy:
	//   - Default target is the canonical container path (/home/composer/.ssh).
	//     It does not exist on dev machines, so EncryptSSHKeys is a no-op.
	//   - Operators who legitimately want a different dir encrypted set
	//     COMPOSER_SSH_DIR=/path/explicitly. No heuristic; opt-in only.
	//   - We never infer the target from $HOME.
	sshDirs := []string{"/home/composer/.ssh"}
	if custom := os.Getenv("COMPOSER_SSH_DIR"); custom != "" {
		sshDirs = append(sshDirs, custom)
	}
	for _, dir := range sshDirs {
		if _, err := os.Stat(dir); err != nil {
			continue // silently skip non-existent paths (the common dev case)
		}
		if n, err := crypto.EncryptSSHKeys(dir); err != nil {
			logger.Warn("failed to encrypt SSH keys", zap.Error(err), zap.String("dir", dir))
		} else if n > 0 {
			logger.Info("encrypted SSH keys at rest", zap.Int("count", n), zap.String("dir", dir))
		}
	}

	// --- Docker Event Listener (bridges Docker events to domain events) ---
	if dockerClient != nil {
		eventListener := docker.NewEventListener(dockerClient, bus)
		eventListener.Start(ctx)
		defer eventListener.Stop()
		logger.Info("docker event listener started")
	}

	// --- Git Client ---
	gitClient := infraGit.NewClient()

	// --- SOPS/age key detection ---
	globalAgeKey := sopsInfra.LoadGlobalAgeKey(cfg.DataDir)
	if globalAgeKey != "" {
		logger.Info("SOPS age key loaded")
	}
	if sopsInfra.IsAvailable() {
		logger.Info("sops binary available for secret decryption")
	}

	// --- Application Services ---
	authSvc := app.NewAuthService(userRepo, sessionRepo, apiKeyRepo)
	// M6: Wire transaction runner for atomic login (revoke+create+update in one tx)
	authSvc.SetTxRunner(store.NewDBTxRunner(db))
	// P2: Wire Valkey cache for session lookups (avoids DB query per authenticated request)
	if valkeyCache != nil {
		authSvc.SetCache(valkeyCache)
		logger.Info("auth session cache enabled via Valkey")
	}
	stackLocks := app.NewStackLocks() // shared across all services that run compose operations
	var gitSvc *app.GitService
	if dockerClient != nil {
		stackSvc = app.NewStackService(stackRepo, gitConfigRepo, dockerClient, compose, bus, logger, cfg.StacksDir, cfg.DataDir, stackLocks)
		gitSvc = app.NewGitService(stackRepo, gitConfigRepo, gitClient, compose, bus, logger, cfg.StacksDir, stackLocks)
	}

	// --- Pipeline Service ---
	pipelineRepo := store.NewPipelineRepo(db.SQL)
	runRepo := store.NewRunRepo(db.SQL)
	var pipelineExecutor *app.PipelineExecutor
	var pipelineSvc *app.PipelineService
	if compose != nil {
		pipelineExecutor = app.NewPipelineExecutor(compose, bus, stackRepo, gitConfigRepo, cfg.StacksDir, stackLocks)
		pipelineSvc = app.NewPipelineService(pipelineRepo, runRepo, pipelineExecutor)
		pipelineSvc.SetLogger(logger)
	}

	// --- Cron Scheduler (for pipeline schedule triggers) ---
	if pipelineSvc != nil {
		cronScheduler := app.NewCronScheduler(pipelineSvc, pipelineRepo, logger)
		cronScheduler.Start(ctx)
		defer cronScheduler.Stop()
		logger.Info("cron scheduler started")
	}

	// --- Webhook + Audit Repos ---
	webhookRepo := store.NewWebhookRepo(db.SQL)
	auditRepo := store.NewAuditRepo(db.SQL)

	// --- Job Manager ---
	jobManager := app.NewJobManager()

	// --- API Server ---
	srv := api.NewServer(api.Deps{
		AuthService:     authSvc,
		StackService:    stackSvc,
		GitService:      gitSvc,
		PipelineService: pipelineSvc,
		UserRepo:        userRepo,
		SessionRepo:     sessionRepo,
		WebhookRepo:     webhookRepo,
		AuditRepo:       auditRepo,
		EventBus:        bus,
		DockerClient:    dockerClient,
		Compose:         compose,
		Jobs:            jobManager,
		DataDir:         cfg.DataDir,
	})

	// --- Embedded frontend (serves web/dist if built) ---
	api.RegisterStaticFiles(srv.Router, composer.FrontendDist)

	// --- HTTP Server ---
	// WriteTimeout=60s applies to regular API endpoints. SSE and WebSocket
	// handlers use http.ResponseController.SetWriteDeadline to extend/disable
	// the deadline per-connection. ReadHeaderTimeout prevents slowloris.
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           srv.Router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Cancellable context for background goroutines
	appCtx, appCancel := context.WithCancel(ctx)

	// --- Background: job cleanup (remove finished jobs older than 1h) ---
	jobManager.StartCleanup(appCtx, 5*time.Minute, 1*time.Hour)
	logger.Info("job cleanup goroutine started")

	// --- Background: session + data cleanup ---
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := authSvc.CleanupExpiredSessions(appCtx); err != nil {
					logger.Warn("session cleanup error", zap.Error(err))
				} else if n > 0 {
					logger.Info("cleaned expired sessions", zap.Int("count", n))
				}
				// P13: Clean old audit log and webhook deliveries (keep 30 days)
				if auditRepo != nil {
					if n, err := auditRepo.CleanupOlderThan(appCtx, 30*24*time.Hour); err != nil {
						logger.Warn("audit cleanup error", zap.Error(err))
					} else if n > 0 {
						logger.Info("cleaned old audit entries", zap.Int("count", n))
					}
				}
				if webhookRepo != nil {
					if n, err := webhookRepo.CleanupDeliveriesOlderThan(appCtx, 30*24*time.Hour); err != nil {
						logger.Warn("delivery cleanup error", zap.Error(err))
					} else if n > 0 {
						logger.Info("cleaned old webhook deliveries", zap.Int("count", n))
					}
				}
			case <-appCtx.Done():
				return
			}
		}
	}()

	// --- Start server ---
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", zap.String("addr", httpSrv.Addr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// --- Wait for shutdown signal or server error ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("shutting down", zap.String("signal", sig.String()))
	case err := <-serverErr:
		logger.Error("server failed, shutting down", zap.Error(err))
	}

	appCancel() // stop background goroutines

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	bus.Close()
	if dockerClient != nil {
		dockerClient.Close()
	}
	// db.Close() handled by defer on line 117
	logger.Info("server stopped")
}
