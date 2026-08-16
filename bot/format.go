package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// tags permitidos por Telegram en HTML (incluye tablas desde Bot API 7.0).
var allowedTagRe = regexp.MustCompile(`(?i)</?(?:b|strong|i|em|u|ins|s|strike|del|code|pre|a|span|blockquote|tg-spoiler|table|thead|tbody|tr|th|td)(?:\s[^>]*)?/?>`)

// sanitizeHTML escapa los caracteres reservados de Telegram (<, >, &) fuera de
// los tags HTML permitidos, de modo que el HTML generado por el LLM no rompa
// el parseo de Telegram.
func sanitizeHTML(text string) string {
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
