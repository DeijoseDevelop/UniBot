package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"unibot/config"
	"unibot/executor"
	"unibot/tools"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared/constant"
)

const systemPrompt = `Eres UniBot, un asistente universitario inteligente.
Reglas:
- Sé conciso pero amigable.
- Si el usuario menciona fecha/hora, usa create_calendar_event.
- Si pregunta por tareas, usa list_classroom_tasks.
- Si quiere guardar información, usa save_note.
- Si envía una imagen, usa upload_image.
- Confirma las acciones con detalles específicos.`

// Orchestrator maneja la conversación con DeepSeek
type Orchestrator struct {
	client   openai.Client
	exec     *executor.Executor
	messages map[int64][]openai.ChatCompletionMessageParamUnion
}

// New crea un nuevo orquestador
func New(exec *executor.Executor) *Orchestrator {
	client := openai.NewClient(
		option.WithAPIKey(config.Cfg.DeepSeekAPIKey),
		option.WithBaseURL("https://api.deepseek.com"),
	)
	return &Orchestrator{
		client:   client,
		exec:     exec,
		messages: make(map[int64][]openai.ChatCompletionMessageParamUnion),
	}
}

// ProcessMessage procesa un mensaje del usuario y devuelve la respuesta
func (o *Orchestrator) ProcessMessage(ctx context.Context, userID int64, message string, imageB64 string) (string, error) {
	// Recuperar o inicializar historial
	history := o.messages[userID]
	if len(history) == 0 {
		history = append(history, openai.SystemMessage(systemPrompt))
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

	// Primera llamada a DeepSeek con tools
	chatParams := openai.ChatCompletionNewParams{
		Model:    "deepseek-v4-flash",
		Messages: history,
		Tools:    tools.ToOpenAITools(),
	}

	resp, err := o.client.Chat.Completions.New(ctx, chatParams)
	if err != nil {
		return "", fmt.Errorf("deepseek API error: %w", err)
	}

	msg := resp.Choices[0].Message

	// Si no hay tool calls, responder directamente
	if len(msg.ToolCalls) == 0 {
		reply := msg.Content
		history = append(history, openai.AssistantMessage(reply))
		o.messages[userID] = history
		return reply, nil
	}

	// Procesar tool calls
	toolResults := make([]openai.ChatCompletionMessageToolCallParam, 0, len(msg.ToolCalls))
	toolMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(msg.ToolCalls))

	for _, tc := range msg.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		result, err := o.exec.Execute(ctx, userID, tc.Function.Name, args)

		var resultJSON string
		if err != nil {
			resultJSON = fmt.Sprintf(`{"error": "%s"}`, err.Error())
		} else {
			b, _ := json.Marshal(result)
			resultJSON = string(b)
		}

		toolResults = append(toolResults, openai.ChatCompletionMessageToolCallParam{
			ID:   tc.ID,
			Type: constant.Function("function"),
			Function: openai.ChatCompletionMessageToolCallFunctionParam{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})

		toolMessages = append(toolMessages, openai.ToolMessage(resultJSON, tc.ID))
	}

	// Segunda llamada con resultados de tools
	finalHistory := append(history, openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			ToolCalls: toolResults,
		},
	})
	finalHistory = append(finalHistory, toolMessages...)

	finalParams := openai.ChatCompletionNewParams{
		Model:    "deepseek-v4-flash",
		Messages: finalHistory,
	}

	finalResp, err := o.client.Chat.Completions.New(ctx, finalParams)
	if err != nil {
		return "", fmt.Errorf("deepseek final call error: %w", err)
	}

	reply := finalResp.Choices[0].Message.Content
	finalHistory = append(finalHistory, openai.AssistantMessage(reply))

	// Limitar historial a últimos 20 mensajes para no exceder contexto
	if len(finalHistory) > 22 { // system + 20 mensajes
		finalHistory = append([]openai.ChatCompletionMessageParamUnion{finalHistory[0]}, finalHistory[len(finalHistory)-20:]...)
	}
	o.messages[userID] = finalHistory

	return reply, nil
}

// ClearHistory limpia el historial de un usuario
func (o *Orchestrator) ClearHistory(userID int64) {
	delete(o.messages, userID)
}
