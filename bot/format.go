package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

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

var preBlockRe = regexp.MustCompile(`(?s)<pre>.*?</pre>`)
var codeBlockRe = regexp.MustCompile("(?s)```[^\n]*\n?(.*?)```")
var inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
var boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
var italicUnderRe = regexp.MustCompile(`(?i)\b_([^_\n]+)_\b`)
var italicStarRe = regexp.MustCompile(`\*([^*\n]+)\*`)
var strikeRe = regexp.MustCompile(`~~([^~\n]+)~~`)
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

func escapeText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), "<", "&lt;"), ">", "&gt;")
}

// markdownToHTML convierte el Markdown natural del LLM (negritas, cursivas,
// código, enlaces, encabezados y tablas con pipes) en HTML válido para
// Telegram. Los bloques <pre> que el modelo ya emite se respetan.
func markdownToHTML(text string) string {
	// Proteger bloques <pre> ya emitidos por el modelo
	preBlocks := []string{}
	text = preBlockRe.ReplaceAllStringFunc(text, func(m string) string {
		preBlocks = append(preBlocks, m)
		return fmt.Sprintf("\x00PRE%d\x01", len(preBlocks)-1)
	})

	// Tablas con pipes → <pre> alineado (monospace)
	text = pipeTablesToPre(text)

	// Bloques de código ``` ... ```
	text = codeBlockRe.ReplaceAllStringFunc(text, func(m string) string {
		content := codeBlockRe.FindStringSubmatch(m)[1]
		return "<pre>" + escapeText(strings.Trim(content, "\n")) + "</pre>"
	})

	// Código inline
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(m string) string {
		inner := inlineCodeRe.FindStringSubmatch(m)[1]
		return "<code>" + escapeText(inner) + "</code>"
	})

	// Enlaces [texto](url)
	text = linkRe.ReplaceAllStringFunc(text, func(m string) string {
		parts := linkRe.FindStringSubmatch(m)
		return `<a href="` + parts[2] + `">` + parts[1] + `</a>`
	})

	// Negrita **texto**
	text = boldRe.ReplaceAllStringFunc(text, func(m string) string {
		return "<b>" + boldRe.FindStringSubmatch(m)[1] + "</b>"
	})

	// Tachado ~~texto~~
	text = strikeRe.ReplaceAllStringFunc(text, func(m string) string {
		return "<s>" + strikeRe.FindStringSubmatch(m)[1] + "</s>"
	})

	// Cursiva _texto_ y *texto*
	text = italicUnderRe.ReplaceAllStringFunc(text, func(m string) string {
		return "<i>" + italicUnderRe.FindStringSubmatch(m)[1] + "</i>"
	})
	text = italicStarRe.ReplaceAllStringFunc(text, func(m string) string {
		return "<i>" + italicStarRe.FindStringSubmatch(m)[1] + "</i>"
	})

	// Encabezados # Título → negrita
	text = headingRe.ReplaceAllString(text, "<b>$1</b>")

	// Restaurar bloques <pre> originales
	for i, p := range preBlocks {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00PRE%d\x01", i), p)
	}
	return text
}

// pipeTablesToPre convierte bloques de líneas con pipes en bloques <pre> con
// columnas alineadas.
func pipeTablesToPre(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); {
		if isTableRow(lines[i]) {
			start := i
			for i < len(lines) && isTableRow(lines[i]) {
				i++
			}
			out = append(out, buildTablePre(lines[start:i]))
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	return strings.Count(line, "|") >= 2
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		t := strings.Trim(strings.ReplaceAll(c, "-", ""), ": ")
		if t != "" {
			return false
		}
	}
	return len(cells) > 0
}

func buildTablePre(rows []string) string {
	parsed := [][]string{}
	widths := []int{}
	for _, row := range rows {
		cells := splitTableRow(row)
		if isSeparatorRow(cells) {
			continue
		}
		parsed = append(parsed, cells)
		for ci, c := range cells {
			w := utf8.RuneCountInString(c)
			if ci >= len(widths) {
				widths = append(widths, w)
			} else if w > widths[ci] {
				widths[ci] = w
			}
		}
	}
	if len(parsed) == 0 {
		return strings.Join(rows, "\n")
	}

	var sb strings.Builder
	sb.WriteString("<pre>")
	for ri, cells := range parsed {
		for ci := 0; ci < len(widths); ci++ {
			cell := ""
			if ci < len(cells) {
				cell = cells[ci]
			}
			if ci > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(pad(cell, widths[ci]))
		}
		if ri < len(parsed)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</pre>")
	return sb.String()
}

func pad(s string, width int) string {
	pad := width - utf8.RuneCountInString(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

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

// sendMessage convierte el Markdown del modelo a HTML de Telegram y lo envía
// con parse_mode HTML. Si el HTML resultante no es válido, reintenta en texto
// plano para no perder el mensaje.
func sendMessage(ctx context.Context, b *bot.Bot, chatID any, text string) error {
	html := sanitizeHTML(markdownToHTML(text))
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      html,
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
