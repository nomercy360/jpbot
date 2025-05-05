package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-playground/validator/v10"
	telegram "github.com/go-telegram/bot"
	"github.com/golang-jwt/jwt/v5"
	echojwt "github.com/labstack/echo-jwt/v4"
	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
	"jpbot/internal/ai"
	"jpbot/internal/db"
	"jpbot/internal/handlers"
	"jpbot/internal/job"
	"jpbot/internal/middleware"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type Config struct {
	Host             string `yaml:"host"`
	Port             int    `yaml:"port"`
	DBPath           string `yaml:"db_path"`
	TelegramBotToken string `yaml:"telegram_bot_token"`
	OpenAIAPIKey     string `yaml:"openai_api_key"`
	GrokAPIKey       string `yaml:"grok_api_key"`
	ExternalURL      string `yaml:"external_url"`
	JWTSecretKey     string `yaml:"jwt_secret_key"`
}

func ReadConfig(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	return &cfg, nil
}

func ValidateConfig(cfg *Config) error {
	validate := validator.New()
	return validate.Struct(cfg)
}

func getAuthConfig(secret string) echojwt.Config {
	return echojwt.Config{
		NewClaimsFunc: func(_ echo.Context) jwt.Claims {
			return new(handlers.JWTClaims)
		},
		SigningKey:             []byte(secret),
		ContinueOnIgnoredError: true,
		ErrorHandler: func(c echo.Context, err error) error {
			var extErr *echojwt.TokenExtractionError
			if !errors.As(err, &extErr) {
				return echo.NewHTTPError(http.StatusUnauthorized, "auth is invalid")
			}

			claims := &handlers.JWTClaims{
				RegisteredClaims: jwt.RegisteredClaims{
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour * 30)),
				},
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

			c.Set("user", token)

			if claims.UID == 0 {
				return echo.NewHTTPError(http.StatusUnauthorized, "auth is invalid")
			}

			return nil
		},
	}
}

func main() {
	configFilePath := "config.yml"
	configFilePathEnv := os.Getenv("CONFIG_FILE_PATH")
	if configFilePathEnv != "" {
		configFilePath = configFilePathEnv
	}

	cfg, err := ReadConfig(configFilePath)
	if err != nil {
		log.Fatalf("error reading configuration: %v", err)
	}

	if err := ValidateConfig(cfg); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	storage, err := db.ConnectDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	openaiClient := ai.NewClient(cfg.GrokAPIKey, cfg.OpenAIAPIKey)

	bot, err := telegram.New(cfg.TelegramBotToken)
	if err != nil {
		log.Fatal(err)
	}

	jobber := job.NewJob(storage, bot)
	go jobber.Run(context.Background())

	handler := handlers.NewHandler(bot, storage, openaiClient, cfg.JWTSecretKey, cfg.TelegramBotToken)

	log.Printf("Authorized on account %d", bot.ID())

	e := echo.New()

	logr := slog.New(slog.NewTextHandler(os.Stdout, nil))

	middleware.Setup(e, logr)

	webhookURL := fmt.Sprintf("%s/webhook", cfg.ExternalURL)
	if ok, err := bot.SetWebhook(context.Background(), &telegram.SetWebhookParams{
		DropPendingUpdates: true,
		URL:                webhookURL,
	}); err != nil {
		log.Fatalf("Failed to set webhook: %v", err)
	} else if !ok {
		log.Fatalf("Failed to set webhook: %v", err)
	}

	e.POST("/webhook", handler.HandleWebhook)
	e.POST("/auth/telegram", handler.TelegramAuth)

	v1 := e.Group("/v1")
	authCfg := getAuthConfig(cfg.JWTSecretKey)

	v1.Use(echojwt.WithConfig(authCfg))
	v1.GET("/leaderboard", handler.HandleLeaderboard)

	port := "8080"
	log.Printf("Starting server on port %s", port)
	if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
