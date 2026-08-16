package bot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"unibot/config"
	"unibot/googleauth"
	"unibot/orchestrator"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"golang.org/x/oauth2"
)

// Bot encapsula el bot de Telegram y el orquestador
type Bot struct {
	bot          *bot.Bot
	orchestrator *orchestrator.Orchestrator
	tokenStore   googleauth.TokenStore
	authStates   map[string]int64
	redirectURL  string
}

// New crea e inicializa el bot de Telegram
func New(orch *orchestrator.Orchestrator, tokenStore googleauth.TokenStore) (*Bot, error) {
	authStates := map[string]int64{}

	opts := []bot.Option{
		bot.WithDefaultHandler(defaultHandler(orch)),
		bot.WithMessageTextHandler("/start", bot.MatchTypeExact, startHandler()),
		bot.WithMessageTextHandler("/auth", bot.MatchTypeExact, authHandler(authStates)),
		bot.WithMessageTextHandler("/revoke", bot.MatchTypeExact, revokeHandler(orch, tokenStore)),
	}

	b, err := bot.New(config.Cfg.TelegramToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	return &Bot{
		bot:          b,
		orchestrator: orch,
		tokenStore:   tokenStore,
		authStates:   authStates,
		redirectURL:  oauthRedirectURL(),
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

		sendMessage(ctx, b, update.Message.Chat.ID, msg)
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
			sendMessage(ctx, b, update.Message.Chat.ID, "🎤 Nota de voz recibida. Transcripción de voz estará disponible próximamente.")
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

		sendMessage(ctx, b, update.Message.Chat.ID, reply)
	}
}

func handlePhoto(ctx context.Context, b *bot.Bot, orch *orchestrator.Orchestrator, update *models.Update) {
	userID := update.Message.From.ID

	// Tomar la foto con mayor resolución
	photo := update.Message.Photo[len(update.Message.Photo)-1]

	// Descargar la foto
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: photo.FileID})
	if err != nil {
		sendMessage(ctx, b, update.Message.Chat.ID, "❌ Error descargando la imagen.")
		return
	}

	// Descargar contenido
	data, err := downloadFile(ctx, b.FileDownloadLink(file))
	if err != nil {
		sendMessage(ctx, b, update.Message.Chat.ID, "❌ Error descargando la imagen.")
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

	sendMessage(ctx, b, update.Message.Chat.ID, reply)
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

// oauthRedirectURL devuelve la URI de redirección del flujo OAuth.
func oauthRedirectURL() string {
	if config.Cfg.GoogleRedirectURI != "" {
		return config.Cfg.GoogleRedirectURI
	}
	if config.Cfg.WebhookURL != "" {
		return config.Cfg.WebhookURL + "/oauth2callback"
	}
	return "http://localhost:" + config.Cfg.Port + "/oauth2callback"
}

// StartPolling inicia el bot en modo long-polling (desarrollo local).
// Borra cualquier webhook previo para que polling funcione.
func (b *Bot) StartPolling(ctx context.Context) {
	if _, err := b.bot.DeleteWebhook(ctx, &bot.DeleteWebhookParams{}); err != nil {
		log.Printf("Warning: failed to delete webhook: %v", err)
	}
	b.bot.Start(ctx)
}

// StartWebhookMode inicia el dispatcher interno que consume las updates
// entregadas al webhook. Es obligatorio en modo webhook; sin él, las updates
// recibidas se descartan (el canal b.updates no tiene consumidor).
func (b *Bot) StartWebhookMode(ctx context.Context) {
	b.bot.StartWebhook(ctx)
}

// SendToUser envía un mensaje directo a un usuario por su ID.
func (b *Bot) SendToUser(ctx context.Context, userID int64, text string) error {
	return sendMessage(ctx, b.bot, userID, text)
}

// CompleteAuth procesa el callback de OAuth: intercambia el code, guarda el
// token en el TokenStore y devuelve el userID de Telegram asociado al state.
func (b *Bot) CompleteAuth(ctx context.Context, state, code string) (int64, error) {
	userID, ok := b.authStates[state]
	if !ok {
		return 0, errors.New("estado OAuth inválido o expirado")
	}
	delete(b.authStates, state)

	if b.tokenStore == nil {
		return 0, errors.New("TokenStore no disponible")
	}

	cfg := googleauth.NewOAuthConfig(config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, b.redirectURL)
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return 0, err
	}

	return userID, b.tokenStore.SaveTokens(ctx, userID, tok)
}

func authHandler(authStates map[string]int64) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}

		userID := update.Message.From.ID
		state := bot.RandomString(24)
		authStates[state] = userID

		cfg := googleauth.NewOAuthConfig(
			config.Cfg.GoogleClientID,
			config.Cfg.GoogleClientSecret,
			oauthRedirectURL(),
		)
		url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)

		msg := "🔐 Para conectar tu cuenta de Google:\n\n" +
			url + "\n\nAbre el enlace, autoriza y vuelve aquí. Te confirmaré cuando esté listo."
		sendMessage(ctx, b, update.Message.Chat.ID, msg)
	}
}

func revokeHandler(orch *orchestrator.Orchestrator, tokenStore googleauth.TokenStore) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}

		userID := update.Message.From.ID
		if tokenStore != nil {
			if revoker, ok := tokenStore.(interface {
				RevokeTokens(context.Context, int64) error
			}); ok {
				if err := revoker.RevokeTokens(ctx, userID); err != nil {
					log.Printf("Error revoking tokens: %v", err)
				}
			}
		}
		orch.ClearHistory(userID)

		msg := "🚫 Desconecté tu cuenta de Google y borré tu historial de conversación."
		sendMessage(ctx, b, update.Message.Chat.ID, msg)
	}
}
