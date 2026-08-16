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
	"unibot/googleauth"
	"unibot/orchestrator"

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
	// TODO: implementar TokenStore real con Supabase
	var tokenStore googleauth.TokenStore // placeholder
	exec := executor.New(tokenStore)
	orch := orchestrator.New(exec)

	// Crear bot de Telegram
	tgBot, err := bot.New(orch)
	if err != nil {
		log.Fatalf("Failed to create telegram bot: %v", err)
	}

	// Configurar webhook
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tgBot.SetWebhook(ctx); err != nil {
		log.Printf("Warning: failed to set webhook: %v", err)
	}

	// Registrar webhook handler
	r.POST("/webhook", gin.WrapF(tgBot.WebhookHandler()))

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
