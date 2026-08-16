package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken       string
	DeepSeekAPIKey      string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRedirectURI   string
	NotionToken         string
	NotionDBID          string
	NotionAnchorPageID  string
	SupabaseURL         string
	SupabaseKey         string
	SupabaseDatabaseURL string
	WebhookURL          string
	Port                string
}

var Cfg *Config

func Load() {
	_ = godotenv.Load()

	Cfg = &Config{
		TelegramToken:       getEnv("TELEGRAM_TOKEN", ""),
		DeepSeekAPIKey:      getEnv("DEEPSEEK_API_KEY", ""),
		GoogleClientID:      getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURI:   getEnv("GOOGLE_REDIRECT_URI", ""),
		NotionToken:         getEnv("NOTION_TOKEN", ""),
		NotionDBID:          getEnv("NOTION_DB_ID", ""),
		NotionAnchorPageID:  getEnv("NOTION_ANCHOR_PAGE_ID", ""),
		SupabaseURL:         getEnv("SUPABASE_URL", ""),
		SupabaseKey:         getEnv("SUPABASE_KEY", ""),
		SupabaseDatabaseURL: getEnv("SUPABASE_DATABASE_URL", ""),
		WebhookURL:          getEnv("WEBHOOK_URL", ""),
		Port:                getEnv("PORT", "8080"),
	}

	if Cfg.TelegramToken == "" || Cfg.DeepSeekAPIKey == "" {
		log.Fatal("TELEGRAM_TOKEN y DEEPSEEK_API_KEY son requeridos")
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
