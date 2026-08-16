package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"unibot/bot"
	"unibot/config"
	"unibot/executor"
	"unibot/notion"
	"unibot/orchestrator"
	"unibot/store"

	"github.com/gin-gonic/gin"
)

func main() {
	// Cargar configuración
	config.Load()

	// Modo release para producción
	gin.SetMode(gin.ReleaseMode)

	// Crear router
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(loggerMiddleware())

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// Inicializar componentes
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer bootCancel()

	var tokenStore *store.TokenStore
	if config.Cfg.SupabaseDatabaseURL != "" {
		ts, err := store.New(bootCtx, config.Cfg.SupabaseDatabaseURL)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		tokenStore = ts
		defer tokenStore.Close()
		log.Println("Database connected (Supabase/Postgres)")
	} else {
		log.Println("Warning: SUPABASE_DATABASE_URL no configurada — tools de Google deshabilitadas")
	}

	notionService := notion.New(config.Cfg.NotionToken, config.Cfg.NotionDBID, config.Cfg.NotionAnchorPageID)

	exec := executor.New(tokenStore, notionService)
	orch := orchestrator.New(exec)

	// Crear bot de Telegram
	tgBot, err := bot.New(orch, tokenStore)
	if err != nil {
		log.Fatalf("Failed to create telegram bot: %v", err)
	}

	// Modo de operación: webhook (producción) o polling (desarrollo local)
	if config.Cfg.WebhookURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := tgBot.SetWebhook(ctx); err != nil {
			log.Printf("Warning: failed to set webhook: %v", err)
		}
		r.POST("/webhook", gin.WrapF(tgBot.WebhookHandler()))
		go tgBot.StartWebhookMode(context.Background())
	} else {
		log.Println("Running in polling mode (sin WEBHOOK_URL)")
		go tgBot.StartPolling(context.Background())
	}

	// Callback de OAuth2 (flujo /auth)
	r.GET("/oauth2callback", func(c *gin.Context) {
		state := c.Query("state")
		code := c.Query("code")
		if code == "" || state == "" {
			c.String(http.StatusBadRequest, "Parámetros inválidos en el callback OAuth.")
			return
		}

		userID, err := tgBot.CompleteAuth(c.Request.Context(), state, code)
		if err != nil {
			c.String(http.StatusBadRequest, "Error de autenticación: %v", err)
			return
		}

		c.String(http.StatusOK, "✅ ¡Cuenta de Google conectada! Ya puedes volver a Telegram.")
		if err := tgBot.SendToUser(c.Request.Context(), userID,
			"✅ ¡Tu cuenta de Google quedó conectada! Ya puedo usar Calendar, Classroom, Drive y Vision."); err != nil {
			log.Printf("Failed to notify auth success: %v", err)
		}
	})

	// Iniciar servidor
	srv := &http.Server{
		Addr:    ":" + config.Cfg.Port,
		Handler: r,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	log.Printf("UniBot server started on port %s", config.Cfg.Port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func loggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		if raw != "" {
			path = path + "?" + raw
		}

		log.Printf("[GIN] %v | %3d | %13v | %15s | %-7s %s",
			start.Format("2006/01/02 - 15:04:05"),
			statusCode,
			latency,
			clientIP,
			method,
			path,
		)
	}
}
