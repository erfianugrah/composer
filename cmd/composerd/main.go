package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2/humacli"
	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/api"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/eventbus"
	"github.com/erfianugrah/composer/internal/infra/store/postgres"
)

// Options defines all CLI flags / env vars for composerd.
type Options struct {
	Port       int    `help:"HTTP port" short:"p" default:"8080"`
	DBUrl      string `help:"Postgres connection URL" default:"postgres://composer:composer@localhost:5432/composer?sslmode=disable"`
	ValkeyURL  string `help:"Valkey connection URL" default:"valkey://localhost:6379"`
	StacksDir  string `help:"Directory for compose stacks" default:"/opt/stacks"`
	DataDir    string `help:"Directory for app data (SSH keys, etc.)" default:"/opt/composer"`
	DockerHost string `help:"Docker/Podman socket (auto-detect if empty)" default:""`
	LogLevel   string `help:"Log level (debug|info|warn|error)" default:"info"`
	LogFormat  string `help:"Log format (json|console)" default:"console"`
}

func buildLogger(opts *Options) (*zap.Logger, error) {
	var cfg zap.Config
	if opts.LogFormat == "json" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	switch opts.LogLevel {
	case "debug":
		cfg.Level.SetLevel(zap.DebugLevel)
	case "warn":
		cfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		cfg.Level.SetLevel(zap.ErrorLevel)
	default:
		cfg.Level.SetLevel(zap.InfoLevel)
	}

	return cfg.Build()
}

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, opts *Options) {
		logger, err := buildLogger(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
			os.Exit(1)
		}
		defer logger.Sync()

		logger.Info("composerd starting",
			zap.Int("port", opts.Port),
			zap.String("stacks_dir", opts.StacksDir),
			zap.String("log_level", opts.LogLevel),
		)

		ctx := context.Background()

		// --- Postgres ---
		db, err := postgres.New(ctx, opts.DBUrl)
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

		// --- Docker ---
		dockerClient, err := docker.NewClient(opts.DockerHost)
		if err != nil {
			logger.Fatal("failed to create docker client", zap.Error(err))
		}
		defer dockerClient.Close()
		logger.Info("docker connected",
			zap.String("runtime", dockerClient.Runtime()),
			zap.String("host", dockerClient.Host()),
		)

		compose := docker.NewCompose(dockerClient.Host())

		// --- Event Bus ---
		bus := eventbus.NewMemoryBus(256)
		defer bus.Close()

		// --- Stacks directory ---
		if err := os.MkdirAll(opts.StacksDir, 0755); err != nil {
			logger.Fatal("failed to create stacks directory", zap.Error(err))
		}

		// --- Application Services ---
		authSvc := app.NewAuthService(userRepo, sessionRepo, apiKeyRepo)
		stackSvc := app.NewStackService(stackRepo, gitConfigRepo, dockerClient, compose, bus, opts.StacksDir)

		// --- API Server ---
		srv := api.NewServer(api.Deps{
			AuthService:  authSvc,
			StackService: stackSvc,
			EventBus:     bus,
			DockerClient: dockerClient,
		})

		// --- HTTP Server ---
		httpSrv := &http.Server{
			Addr:         fmt.Sprintf(":%d", opts.Port),
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

		hooks.OnStart(func() {
			go func() {
				logger.Info("HTTP server listening", zap.String("addr", httpSrv.Addr))
				if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Fatal("server failed", zap.Error(err))
				}
			}()

			// Wait for shutdown signal
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
			dockerClient.Close()
			db.Close()
			logger.Info("server stopped")
		})
	})

	cli.Run()
}
