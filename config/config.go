package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramToken         string
	DeepSeekAPIKey        string
	OpenAIAPIKey          string
	GoogleClientID        string
	GoogleClientSecret    string
	GoogleRedirectURI     string
	NotionToken           string
	NotionDBID            string
	NotionAnchorPageID    string
	SupabaseURL           string
	SupabaseKey           string
	SupabaseDatabaseURL   string
	FolderApuntesID       string
	FolderTareasID        string
	FolderDocumentosID    string
	WebhookURL            string
	Port                  string
	RateLimitMaxPerMinute int
	ReminderEnabled       bool
	ReminderHoursAhead    int
}

var Cfg *Config

func Load() {
	_ = godotenv.Load()

	Cfg = &Config{
		TelegramToken:         getEnv("TELEGRAM_TOKEN", ""),
		DeepSeekAPIKey:        getEnv("DEEPSEEK_API_KEY", ""),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		GoogleClientID:        getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:    getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURI:     getEnv("GOOGLE_REDIRECT_URI", ""),
		NotionToken:           getEnv("NOTION_TOKEN", ""),
		NotionDBID:            getEnv("NOTION_DB_ID", ""),
		NotionAnchorPageID:    getEnv("NOTION_ANCHOR_PAGE_ID", ""),
		SupabaseURL:           getEnv("SUPABASE_URL", ""),
		SupabaseKey:           getEnv("SUPABASE_KEY", ""),
		SupabaseDatabaseURL:   getEnv("SUPABASE_DATABASE_URL", ""),
		FolderApuntesID:       getEnv("FOLDER_APUNTES_ID", ""),
		FolderTareasID:        getEnv("FOLDER_TAREAS_ID", ""),
		FolderDocumentosID:    getEnv("FOLDER_DOCUMENTOS_ID", ""),
		WebhookURL:            getEnv("WEBHOOK_URL", ""),
		Port:                  getEnv("PORT", "8080"),
		RateLimitMaxPerMinute: getEnvInt("RATE_LIMIT_MAX_PER_MIN", 30),
		ReminderEnabled:       getEnvBool("REMINDER_ENABLED", true),
		ReminderHoursAhead:    getEnvInt("REMINDER_HOURS_AHEAD", 24),
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

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
