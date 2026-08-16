package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// tags permitidos por Telegram en parse_mode HTML (según docs oficiales:
// b/strong, i/em, u/ins, s/strike/del, a, code, pre, blockquote, tg-spoiler,
// tg-emoji, tg-time. Las tablas <table> NO están soportadas en sendMessage).
var allowedTagRe = regexp.MustCompile(`(?i)</?(?:b|strong|i|em|u|ins|s|strike|del|a|code|pre|blockquote|tg-spoiler)(?:\s[^>]*)?/?>`)

// tags HTML no soportados por Telegram: se eliminan conservando su contenido.
var disallowedTagRe = regexp.MustCompile(`(?i)</?(?:table|thead|tbody|tr|th|td|span|div|ul|ol|li|img|figure|figcaption|video|script|style|h[1-6])(?:\s[^>]*)?/?>`)

var tdCloseRe = regexp.MustCompile(`(?i)</(td|th)>`)
var trCloseRe = regexp.MustCompile(`(?i)</(tr|thead|tbody|table)>`)
var blockCloseRe = regexp.MustCompile(`(?i)</(p|div|li|ul|ol|h[1-6])>|(?i)<br\s*/?>`)

// sanitizeHTML convierte el HTML del LLM en HTML válido para Telegram:
// las tablas no soportadas se transforman en texto con pipes y saltos de
// línea, los tags no permitidos se eliminan, y los caracteres reservados
// (<, >, &) fuera de los tags permitidos se escapan.
func sanitizeHTML(text string) string {
	// Tablas no soportadas → texto legible
	text = tdCloseRe.ReplaceAllString(text, " | ")
	text = trCloseRe.ReplaceAllString(text, "\n")
	text = blockCloseRe.ReplaceAllString(text, "\n")
	text = disallowedTagRe.ReplaceAllString(text, "")

	placeholders := []string{}
	repl := func(m string) string {
		placeholders = append(placeholders, m)
		return fmt.Sprintf("\x00TAG%d\x01", len(placeholders)-1)
	}
	protected := allowedTagRe.ReplaceAllStringFunc(text, repl)

	escaped := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(protected, "&", "&amp;"), "<", "&lt;"), ">", "&gt;")

	for i, p := range placeholders {
		escaped = strings.ReplaceAll(escaped, fmt.Sprintf("\x00TAG%d\x01", i), p)
	}
	return escaped
}

// sendMessage envía un mensaje con formato HTML de Telegram. Si el HTML del
// modelo no es válido, reintenta en texto plano para no perder el mensaje.
func sendMessage(ctx context.Context, b *bot.Bot, chatID any, text string) error {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      sanitizeHTML(text),
		ParseMode: models.ParseModeHTML,
	}); err == nil {
		return nil
	}

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	})
	return err
}
