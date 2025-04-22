package main

import (
	"context"
	"fmt"
	"github.com/go-playground/validator/v10"
	telegram "github.com/go-telegram/bot"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gopkg.in/yaml.v3"
	"jpbot/internal/ai"
	"jpbot/internal/db"
	"jpbot/internal/handlers"
	"jpbot/internal/job"
	"log"
	"net/http"
	"os"
)

type Config struct {
	Host             string `yaml:"host"`
	Port             int    `yaml:"port"`
	DBPath           string `yaml:"db_path"`
	TelegramBotToken string `yaml:"telegram_bot_token"`
	OpenAIAPIKey     string `yaml:"openai_api_key"`
	GrokAPIKey       string `yaml:"grok_api_key"`
	ExternalURL      string `yaml:"external_url"`
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

	jobber := job.NewJob(storage, openaiClient, bot)
	go jobber.Run(context.Background())

	handler := handlers.NewHandler(bot, storage, openaiClient)

	log.Printf("Authorized on account %d", bot.ID())

	e := echo.New()
	e.Use(middleware.Logger())
	//e.Use(middleware.Recover())

	webhookURL := fmt.Sprintf("%s/webhook", cfg.ExternalURL)
	if ok, err := bot.SetWebhook(context.Background(), &telegram.SetWebhookParams{
		DropPendingUpdates: true,
		URL:                webhookURL,
	}); err != nil {
		log.Fatalf("Failed to set webhook: %v", err)
	} else if !ok {
		log.Fatalf("Failed to set webhook: %v", err)
	}

	// Роуты
	e.POST("/webhook", handler.HandleWebhook)

	// Запуск сервера
	port := "8080" // Или другой порт
	log.Printf("Starting server on port %s", port)
	if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
