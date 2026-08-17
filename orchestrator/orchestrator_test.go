package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go"
)

func TestExtractInlineToolCalls(t *testing.T) {
	content := `Voy a crear el evento.
<tool_calls>
<invoke name="create_calendar_event">
<parameter name="title">Parcial de Cálculo</parameter>
<parameter name="date">2026-08-17T08:00:00-05:00</parameter>
<parameter name="duration_minutes">120</parameter>
<parameter name="repite">false</parameter>
</invoke>
<invoke name="save_note">
<parameter name="title">Apunte</parameter>
<parameter name="content">texto de la nota</parameter>
</invoke>
</tool_calls>`

	calls := extractInlineToolCalls(content)
	if len(calls) != 2 {
		t.Fatalf("esperaba 2 calls, obtuve %d", len(calls))
	}
	c := calls[0]
	if c.name != "create_calendar_event" {
		t.Errorf("name = %q", c.name)
	}
	if c.id == "" {
		t.Error("id vacío")
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(c.args), &args); err != nil {
		t.Fatalf("args no es JSON válido: %v", err)
	}
	if args["title"] != "Parcial de Cálculo" {
		t.Errorf("title = %v", args["title"])
	}
	if args["duration_minutes"] != float64(120) {
		t.Errorf("duration_minutes = %v (debe ser número)", args["duration_minutes"])
	}
}

func TestCleanToolSequence(t *testing.T) {
	build := func(role string) openai.ChatCompletionMessageParamUnion {
		switch role {
		case "tool":
			return openai.ToolMessage(`{"ok":true}`, "call_1")
		case "assistant_tools":
			return openai.ChatCompletionMessageParamUnion{OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				ToolCalls: []openai.ChatCompletionMessageToolCallParam{{ID: "call_1"}},
			}}
		default:
			return openai.UserMessage(role)
		}
	}

	// Huérfano al inicio debe eliminarse
	msgs := []openai.ChatCompletionMessageParamUnion{
		build("tool"),
		build("assistant_tools"),
		build("tool"),
		build("user"),
	}
	cleaned := cleanToolSequence(msgs)
	if len(cleaned) != 3 {
		t.Fatalf("esperaba 3 mensajes, obtuve %d", len(cleaned))
	}
	if cleaned[0].OfAssistant == nil || len(cleaned[0].OfAssistant.ToolCalls) != 1 {
		t.Errorf("el primer mensaje debería ser assistant con tool_calls")
	}
	if cleaned[1].OfTool == nil {
		t.Errorf("el segundo mensaje debería ser tool")
	}
}
