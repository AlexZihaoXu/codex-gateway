package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"codex-gateway/internal/app"
	adminauth "codex-gateway/internal/auth"
	"codex-gateway/internal/chats"
	"codex-gateway/internal/codex"
	"codex-gateway/internal/config"
	"codex-gateway/internal/dashboard"
	"codex-gateway/internal/projects"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	configureLogger(configuration.LogLevel)

	if err := os.MkdirAll(configuration.DataDirectory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(configuration.DataDirectory, 0o700); err != nil {
		return err
	}
	authentication, err := adminauth.Open(adminauth.Options{
		DataSourceName:     filepath.Join(configuration.DataDirectory, "codex-gateway.db"),
		AdministratorEmail: configuration.Firebase.AdministratorEmail,
		AllowedEmails:      configuration.Firebase.AllowedEmails,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := authentication.Close(); err != nil {
			slog.Error("close authentication database", "error", err)
		}
	}()
	identityVerifier, err := adminauth.NewFirebaseVerifier(adminauth.FirebaseVerifierOptions{
		ProjectID: configuration.Firebase.ProjectID,
	})
	if err != nil {
		return err
	}
	projectService, err := projects.Open(projects.Options{
		DataSourceName: filepath.Join(configuration.DataDirectory, "codex-gateway.db"),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := projectService.Close(); err != nil {
			slog.Error("close project database", "error", err)
		}
	}()
	chatHistory, err := chats.Open(chats.Options{
		DataSourceName: filepath.Join(configuration.DataDirectory, "codex-gateway.db"),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := chatHistory.Close(); err != nil {
			slog.Error("close chat history database", "error", err)
		}
	}()
	codexClient, err := codex.Start(context.Background(), codex.StartOptions{
		Version:       version,
		DataDirectory: configuration.DataDirectory,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := codexClient.Close(); err != nil {
			slog.Error("close Codex transport", "error", err)
		}
	}()
	dashboardHandler, err := dashboard.NewHandler()
	if err != nil {
		return err
	}
	handler, err := app.New(app.Options{
		Version:          version,
		Dashboard:        dashboardHandler,
		Authentication:   authentication,
		IdentityVerifier: identityVerifier,
		Firebase: app.FirebaseWebConfig{
			APIKey:     configuration.Firebase.APIKey,
			AuthDomain: configuration.Firebase.AuthDomain,
			ProjectID:  configuration.Firebase.ProjectID,
			AppID:      configuration.Firebase.AppID,
		},
		Codex:       codexClient,
		Projects:    projectService,
		ChatHistory: chatHistory,
		Inference:   codexClient,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              configuration.Address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverError := make(chan error, 1)
	go func() {
		slog.Info("gateway listening", "address", configuration.Address, "version", version)
		serverError <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return nil
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func configureLogger(levelName string) {
	level := slog.LevelInfo
	switch levelName {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
