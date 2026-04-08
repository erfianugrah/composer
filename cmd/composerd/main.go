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
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
	infraGit "github.com/erfianugrah/composer/internal/infra/git"
	"github.com/erfianugrah/composer/internal/infra/notify"
	"github.com/erfianugrah/composer/internal/infra/store/postgres"
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
		DBUrl:        envStr("COMPOSER_DB_URL", "postgres://composer:composer@localhost:5432/composer?sslmode=disable"),
		ValkeyURL:    envStr("COMPOSER_VALKEY_URL", "valkey://localhost:6379"),
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

	// --- Postgres ---
	db, err := postgres.New(ctx, cfg.DBUrl)
	if err != nil {
		logger.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer db.Close()
	logger.Info("postgres connected, migrations applied")

	// --- Repositories ---
	userRepo := postgres.NewUserRepo(db.Pool)
	sessionRepo := postgres.NewSessionRepo(db.Pool)
	apiKeyRepo := postgres.NewAPIKeyRepo(db.Pool)
	stackRepo := postgres.NewStackRepo(db.Pool)
	gitConfigRepo := postgres.NewGitConfigRepo(db.Pool)

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
		compose = docker.NewCompose(dockerClient.Host())
	}

	// --- Valkey Cache (optional) ---
	valkeyCache, err := cache.New(ctx, cfg.ValkeyURL)
	if err != nil {
		logger.Warn("valkey not available, caching disabled", zap.Error(err))
	} else if valkeyCache != nil {
		defer valkeyCache.Close()
		logger.Info("valkey connected")
	}
	_ = valkeyCache // used by auth middleware for session caching

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

	// --- Docker Event Listener (bridges Docker events to domain events) ---
	if dockerClient != nil {
		eventListener := docker.NewEventListener(dockerClient, bus)
		eventListener.Start(ctx)
		defer eventListener.Stop()
		logger.Info("docker event listener started")
	}

	// --- Git Client ---
	gitClient := infraGit.NewClient()

	// --- Application Services ---
	authSvc := app.NewAuthService(userRepo, sessionRepo, apiKeyRepo)
	var gitSvc *app.GitService
	if dockerClient != nil {
		stackSvc = app.NewStackService(stackRepo, gitConfigRepo, dockerClient, compose, bus, cfg.StacksDir)
		gitSvc = app.NewGitService(stackRepo, gitConfigRepo, gitClient, compose, bus, cfg.StacksDir)
	}

	// --- Pipeline Service ---
	pipelineRepo := postgres.NewPipelineRepo(db.Pool)
	runRepo := postgres.NewRunRepo(db.Pool)
	var pipelineExecutor *app.PipelineExecutor
	var pipelineSvc *app.PipelineService
	if compose != nil {
		pipelineExecutor = app.NewPipelineExecutor(compose, bus)
		pipelineSvc = app.NewPipelineService(pipelineRepo, runRepo, pipelineExecutor)
	}

	// --- Cron Scheduler (for pipeline schedule triggers) ---
	if pipelineSvc != nil {
		cronScheduler := app.NewCronScheduler(pipelineSvc, pipelineRepo, logger)
		cronScheduler.Start(ctx)
		defer cronScheduler.Stop()
		logger.Info("cron scheduler started")
	}

	// --- Webhook + Audit Repos ---
	webhookRepo := postgres.NewWebhookRepo(db.Pool)
	auditRepo := postgres.NewAuditRepo(db.Pool)

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
	})

	// --- Embedded frontend (serves web/dist if built) ---
	api.RegisterStaticFiles(srv.Router, composer.FrontendDist)

	// --- HTTP Server ---
	httpSrv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      srv.Router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// --- Background: session cleanup ---
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := authSvc.CleanupExpiredSessions(ctx); err != nil {
				logger.Warn("session cleanup error", zap.Error(err))
			} else if n > 0 {
				logger.Info("cleaned expired sessions", zap.Int("count", n))
			}
		}
	}()

	// --- Start server ---
	go func() {
		logger.Info("HTTP server listening", zap.String("addr", httpSrv.Addr))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// --- Wait for shutdown signal ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	bus.Close()
	if dockerClient != nil {
		dockerClient.Close()
	}
	db.Close()
	logger.Info("server stopped")
}
