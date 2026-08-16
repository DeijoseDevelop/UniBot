package bot

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"unibot/config"
	"unibot/orchestrator"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Bot encapsula el bot de Telegram y el orquestador
type Bot struct {
	bot          *bot.Bot
	orchestrator *orchestrator.Orchestrator
}

// New crea e inicializa el bot de Telegram
func New(orch *orchestrator.Orchestrator) (*Bot, error) {
	opts := []bot.Option{
		bot.WithDefaultHandler(defaultHandler(orch)),
		bot.WithMessageTextHandler("/start", bot.MatchTypeExact, startHandler()),
	}

	b, err := bot.New(config.Cfg.TelegramToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &Bot{
		bot:          b,
		orchestrator: orch,
	}, nil
}

// SetWebhook configura el webhook en Telegram
func (b *Bot) SetWebhook(ctx context.Context) error {
	_, err := b.bot.SetWebhook(ctx, &bot.SetWebhookParams{
		URL: fmt.Sprintf("%s/webhook", config.Cfg.WebhookURL),
	})
	return err
}

// WebhookHandler devuelve el handler HTTP para el webhook
func (b *Bot) WebhookHandler() http.HandlerFunc {
	return b.bot.WebhookHandler()
}

// ProcessUpdate procesa una actualización manualmente (útil para testing)
func (b *Bot) ProcessUpdate(ctx context.Context, update *models.Update) {
	b.bot.ProcessUpdate(ctx, update)
}

func startHandler() bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		msg := `🎓 ¡Hola! Soy UniBot, tu asistente universitario.

Puedo ayudarte a:
📅 Crear eventos en tu calendario
🎓 Consultar tareas de Classroom
📝 Guardar notas y apuntes
📷 Procesar fotos del pizarrón

Solo escríbeme lo que necesites.`

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   msg,
		})
	}
}

func defaultHandler(orch *orchestrator.Orchestrator) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}

		userID := update.Message.From.ID

		// Indicador de "escribiendo..."
		b.SendChatAction(ctx, &bot.SendChatActionParams{
			ChatID: update.Message.Chat.ID,
			Action: models.ChatActionTyping,
		})

		// Fotos: descargar, codificar y procesar multimodal
		if len(update.Message.Photo) > 0 {
			handlePhoto(ctx, b, orch, update)
			return
		}

		// Notas de voz: placeholder
		if update.Message.Voice != nil {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "🎤 Nota de voz recibida. Transcripción de voz estará disponible próximamente.",
			})
			return
		}

		message := update.Message.Text

		// Procesar con timeout de 30 segundos
		procCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		reply, err := orch.ProcessMessage(procCtx, userID, message, "")
		if err != nil {
			log.Printf("Error processing message: %v", err)
			reply = "❌ Lo siento, ocurrió un error procesando tu mensaje. Intenta de nuevo."
		}

		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   reply,
		})
	}
}

func handlePhoto(ctx context.Context, b *bot.Bot, orch *orchestrator.Orchestrator, update *models.Update) {
	userID := update.Message.From.ID

	// Tomar la foto con mayor resolución
	photo := update.Message.Photo[len(update.Message.Photo)-1]

	// Descargar la foto
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: photo.FileID})
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Error descargando la imagen.",
		})
		return
	}

	// Descargar contenido
	data, err := downloadFile(ctx, b.FileDownloadLink(file))
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Error descargando la imagen.",
		})
		return
	}

	imageB64 := base64.StdEncoding.EncodeToString(data)
	caption := update.Message.Caption
	if caption == "" {
		caption = "Procesa esta imagen"
	}

	procCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	reply, err := orch.ProcessMessage(procCtx, userID, caption, imageB64)
	if err != nil {
		log.Printf("Error processing photo: %v", err)
		reply = "❌ Error procesando la imagen."
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   reply,
	})
}

func downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
