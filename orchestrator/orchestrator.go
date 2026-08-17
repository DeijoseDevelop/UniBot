package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"unibot/config"
	"unibot/executor"
	"unibot/store"
	"unibot/tools"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared/constant"
)

const systemPromptTemplate = `Eres UniBot, un asistente universitario inteligente.
Hoy es %s.
Reglas:
- Sé conciso pero amigable.
- Si el usuario menciona fecha/hora, usa create_calendar_event.
- Si pregunta por sus clases o cursos, usa list_classroom_courses. Si menciona un año (ej. "este año"), pásalo en year para filtrar.
- Si pregunta por tareas, usa list_classroom_tasks. Las tareas vencidas se excluyen por defecto (include_overdue=false). Si menciona un año, usa year. Nunca inventes filtros que la tool no tenga.
- Si pregunta los detalles de UNA tarea específica (descripción, materiales, estado de entrega), usa get_classroom_task con el task_id que devuelve list_classroom_tasks.
- Para consultar eventos del calendario usa list_calendar_events (con start_date/end_date si el usuario da fechas); para los detalles de uno usa get_calendar_event.
- Para editar o mover un evento existente usa update_calendar_event; para borrarlo delete_calendar_event (pide antes el ID con list_calendar_events si no lo tienes).
- Para buscar archivos en Drive usa search_drive_files (query = texto del nombre); para ver los detalles o el contenido de uno usa get_drive_file (include_content=true extrae el texto si es Docs/Sheets/Slides).
- Para consultar notas guardadas usa query_notes (query = texto del título); para el contenido completo de una usa get_note.
- Si quiere guardar información, usa save_note.
- Si envía una imagen, usa upload_image.
- Si pide un resumen de su semana (eventos, tareas y notas), usa get_weekly_summary.
- Confirma las acciones con detalles específicos.
- NUNCA escribas llamadas a herramientas como texto (ni <tool_calls> ni <invoke>): las herramientas se ejecutan automáticamente por el sistema.
- Para días relativos (martes, próximo lunes, etc.) usa la fecha de hoy como referencia.
- Para eventos usa el formato RFC3339 con offset -05:00.
- Si la tool devuelve filtered_count mayor a 0, menciónalo (se descartaron N elementos con los filtros).
FORMATO DE TUS RESPUESTAS:
- Usa Markdown natural para resaltar: **negrita**, _cursiva_, ~~tachado~~, [texto](url), # Encabezados, y código entre comillas invertidas. El bot lo convierte automáticamente a HTML de Telegram.
- Para tablas de datos (tareas, cursos, eventos) usa <pre> con columnas alineadas en monospace, con el título en negrita antes del bloque. NO uses tablas con pipes (|) ni <table>.
- NO uses <br>: usa saltos de línea normales.`

var systemPrompt = buildSystemPrompt()

func buildSystemPrompt() string {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		loc = time.Local
	}
	today := time.Now().In(loc)
	return fmt.Sprintf(systemPromptTemplate, today.Format("2 de January de 2006"))
}

// ConversationStore define la persistencia del historial de conversación.
type ConversationStore interface {
	GetConversation(ctx context.Context, userID int64) ([]store.StoredMessage, error)
	SaveConversation(ctx context.Context, userID int64, msgs []store.StoredMessage) error
	DeleteConversation(ctx context.Context, userID int64) error
}

// Orchestrator maneja la conversación con DeepSeek
type Orchestrator struct {
	client   openai.Client
	exec     *executor.Executor
	store    ConversationStore
	messages map[int64][]openai.ChatCompletionMessageParamUnion
}

// New crea un nuevo orquestador
func New(exec *executor.Executor, conversationStore ConversationStore) *Orchestrator {
	client := openai.NewClient(
		option.WithAPIKey(config.Cfg.DeepSeekAPIKey),
		option.WithBaseURL("https://api.deepseek.com"),
		option.WithJSONSet("thinking", map[string]string{"type": "disabled"}),
	)
	return &Orchestrator{
		client:   client,
		exec:     exec,
		store:    conversationStore,
		messages: make(map[int64][]openai.ChatCompletionMessageParamUnion),
	}
}

// ProcessMessage procesa un mensaje del usuario y devuelve la respuesta
func (o *Orchestrator) ProcessMessage(ctx context.Context, userID int64, message string, imageB64 string) (string, error) {
	// Recuperar o inicializar historial
	history := o.messages[userID]
	if len(history) == 0 {
		history = append(history, openai.SystemMessage(systemPrompt))
		if o.store != nil {
			if stored, err := o.store.GetConversation(ctx, userID); err == nil && len(stored) > 0 {
				loaded := make([]openai.ChatCompletionMessageParamUnion, 0, len(stored))
				for _, m := range stored {
					loaded = append(loaded, fromStored(m))
				}
				// Eliminar tool messages huérfanos de historiales viejos
				history = append(history, cleanToolSequence(loaded)...)
			}
		}
	}

	// Construir mensaje del usuario
	userMsg := openai.UserMessage(message)
	if imageB64 != "" {
		// Para imágenes, usamos content multimodal
		userMsg = openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
			openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: fmt.Sprintf("data:image/jpeg;base64,%s", imageB64),
			}),
			openai.TextContentPart(message),
		})
	}
	history = append(history, userMsg)
	history = cleanToolSequence(history)

	// Llamadas a DeepSeek en rondas: cada ronda puede devolver tool calls
	// estructurados o en formato texto (Codex: <tool_calls><invoke>...).
	for round := 0; round < 5; round++ {
		params := openai.ChatCompletionNewParams{
			Model:    "deepseek-v4-flash",
			Messages: history,
			Tools:    tools.ToOpenAITools(),
		}
		resp, err := o.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("deepseek API error: %w", err)
		}

		msg := resp.Choices[0].Message

		// Recopilar tool calls (estructurados + inline en formato Codex)
		calls := collectToolCalls(msg)
		if len(calls) == 0 {
			reply := msg.Content
			history = append(history, openai.AssistantMessage(reply))
			history = trimHistory(history)
			o.messages[userID] = history
			o.persistHistory(ctx, userID, history)
			return reply, nil
		}

		// Ejecutar las tools y construir los mensajes de resultado
		toolResults := make([]openai.ChatCompletionMessageToolCallParam, 0, len(calls))
		toolMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(calls))

		for _, c := range calls {
			result, err := o.exec.Execute(ctx, userID, c.name, json.RawMessage(c.args))

			var resultJSON string
			if err != nil {
				resultJSON = fmt.Sprintf(`{"error": "%s"}`, err.Error())
			} else {
				b, _ := json.Marshal(result)
				resultJSON = string(b)
			}

			toolResults = append(toolResults, openai.ChatCompletionMessageToolCallParam{
				ID:   c.id,
				Type: constant.Function("function"),
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      c.name,
					Arguments: c.args,
				},
			})
			toolMessages = append(toolMessages, openai.ToolMessage(resultJSON, c.id))
		}

		// El mensaje del assistant se reconstruye con las tool calls reales
		// (se descarta el XML inline que el modelo pudo escribir en content)
		history = append(history, openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				ToolCalls: toolResults,
			},
		})
		history = append(history, toolMessages...)
	}

	return "", errors.New("demasiadas rondas de tool calls")
}

// persistHistory guarda el historial (sin el system prompt) en el store.
func (o *Orchestrator) persistHistory(ctx context.Context, userID int64, history []openai.ChatCompletionMessageParamUnion) {
	if o.store == nil {
		return
	}
	stored := make([]store.StoredMessage, 0, len(history))
	for _, m := range history {
		if m.OfSystem != nil {
			continue
		}
		stored = append(stored, toStored(m))
	}
	if err := o.store.SaveConversation(ctx, userID, stored); err != nil {
		log.Printf("Error persisting conversation for user %d: %v", userID, err)
	}
}

// ClearHistory limpia el historial de un usuario
func (o *Orchestrator) ClearHistory(userID int64) {
	delete(o.messages, userID)
	if o.store != nil {
		_ = o.store.DeleteConversation(context.Background(), userID)
	}
}

// toolCall representa una llamada a tool pendiente de ejecutar.
type toolCall struct {
	id   string
	name string
	args string
}

// collectToolCalls recopila las tool calls de un mensaje del modelo:
// las estructuradas (msg.ToolCalls) y las inline en formato Codex que
// DeepSeek a veces escribe como texto (<tool_calls><invoke name="...">).
func collectToolCalls(msg openai.ChatCompletionMessage) []toolCall {
	calls := []toolCall{}
	for _, tc := range msg.ToolCalls {
		calls = append(calls, toolCall{id: tc.ID, name: tc.Function.Name, args: tc.Function.Arguments})
	}
	if len(msg.ToolCalls) == 0 {
		for _, ic := range extractInlineToolCalls(msg.Content) {
			calls = append(calls, toolCall{id: ic.id, name: ic.name, args: ic.args})
		}
	}
	return calls
}

type inlineToolCall struct {
	id   string
	name string
	args string
}

var invokeRe = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)"\s*>(.*?)</invoke>`)
var parameterRe = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)"\s*>(.*?)</parameter>`)

// extractInlineToolCalls parsea las tool calls en formato Codex:
//
//	<tool_calls>
//	<invoke name="create_calendar_event">
//	<parameter name="title">Parcial</parameter>
//	...
//	</invoke>
//	</tool_calls>
func extractInlineToolCalls(content string) []inlineToolCall {
	matches := invokeRe.FindAllStringSubmatch(content, -1)
	calls := make([]inlineToolCall, 0, len(matches))
	for i, m := range matches {
		params := map[string]interface{}{}
		for _, p := range parameterRe.FindAllStringSubmatch(m[2], -1) {
			val := strings.TrimSpace(p[2])
			var parsed interface{}
			if err := json.Unmarshal([]byte(val), &parsed); err == nil {
				params[p[1]] = parsed
			} else {
				params[p[1]] = val
			}
		}
		args, _ := json.Marshal(params)
		calls = append(calls, inlineToolCall{
			id:   fmt.Sprintf("call_inline_%d", i),
			name: m[1],
			args: string(args),
		})
	}
	return calls
}

// cleanToolSequence elimina los mensajes con rol tool que no tienen un
// mensaje assistant con tool_calls inmediatamente anterior (tool huérfanos).
func cleanToolSequence(msgs []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	allowTool := false
	for _, m := range msgs {
		if m.OfTool != nil {
			if allowTool {
				out = append(out, m)
			}
			continue
		}
		allowTool = m.OfAssistant != nil && len(m.OfAssistant.ToolCalls) > 0
		out = append(out, m)
	}
	return out
}

// trimHistory limita el historial a los últimos 20 mensajes sin cortar a
// mitad de una ronda de tools (evita tool messages huérfanos).
func trimHistory(history []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	const maxMsgs = 22 // system + 21
	if len(history) <= maxMsgs {
		return history
	}
	trimmed := append([]openai.ChatCompletionMessageParamUnion{history[0]}, history[len(history)-maxMsgs+1:]...)
	return cleanToolSequence(trimmed)
}

// toStored convierte un mensaje de la API en su representación persistible.
func toStored(m openai.ChatCompletionMessageParamUnion) store.StoredMessage {
	sm := store.StoredMessage{}
	switch {
	case m.OfUser != nil:
		sm.Role = "user"
		sm.Content = contentText(m.OfUser.Content)
	case m.OfAssistant != nil:
		sm.Role = "assistant"
		sm.Content = contentText(m.OfAssistant.Content)
		for _, tc := range m.OfAssistant.ToolCalls {
			if raw, err := json.Marshal(tc); err == nil {
				sm.ToolCalls = append(sm.ToolCalls, raw)
			}
		}
	case m.OfTool != nil:
		sm.Role = "tool"
		sm.Content = contentText(m.OfTool.Content)
		sm.ToolCallID = m.OfTool.ToolCallID
	case m.OfFunction != nil:
		sm.Role = "function"
		sm.Content = m.OfFunction.Content.Value
	}
	return sm
}

// fromStored reconstruye un mensaje de la API desde su representación persistida.
func fromStored(m store.StoredMessage) openai.ChatCompletionMessageParamUnion {
	switch m.Role {
	case "user":
		return openai.UserMessage(m.Content)
	case "tool":
		return openai.ToolMessage(m.Content, m.ToolCallID)
	case "function":
		return openai.ChatCompletionMessageParamOfFunction(m.Content, "")
	case "assistant":
		if len(m.ToolCalls) == 0 {
			return openai.AssistantMessage(m.Content)
		}
		toolCalls := make([]openai.ChatCompletionMessageToolCallParam, 0, len(m.ToolCalls))
		for _, raw := range m.ToolCalls {
			var tc openai.ChatCompletionMessageToolCallParam
			if err := json.Unmarshal(raw, &tc); err == nil {
				toolCalls = append(toolCalls, tc)
			}
		}
		// El content de un assistant con tool_calls debe ir como null, no "".
		asm := &openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
		if m.Content != "" {
			asm.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(m.Content)}
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: asm}
	}
	return openai.SystemMessage(m.Content)
}

func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case openai.ChatCompletionUserMessageParamContentUnion:
		return c.OfString.Value
	case openai.ChatCompletionAssistantMessageParamContentUnion:
		return c.OfString.Value
	case openai.ChatCompletionToolMessageParamContentUnion:
		return c.OfString.Value
	default:
		return ""
	}
}
