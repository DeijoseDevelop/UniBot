package tools

import (
	"encoding/json"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"
)

// ToolDefinition representa una herramienta disponible para el LLM
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
}

type FunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// AllTools devuelve el slice de herramientas registradas
func AllTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "create_calendar_event",
				Description: "Crea un evento en Google Calendar",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"title": {"type": "string"},
						"date": {"type": "string", "format": "date-time"},
						"description": {"type": "string"},
						"duration_minutes": {"type": "integer", "default": 60}
					},
					"required": ["title", "date"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "list_classroom_courses",
				Description: "Lista los cursos de Google Classroom en los que el usuario está inscrito (nombre, id y sección)",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "list_classroom_tasks",
				Description: "Lista las tareas pendientes de todos los cursos de Google Classroom del usuario. course_id es opcional: si se omite, consulta todos los cursos",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"course_id": {"type": "string", "description": "ID del curso (opcional). Si se omite, lista las tareas de todos los cursos"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "save_note",
				Description: "Guarda una nota en Notion",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"title": {"type": "string"},
						"content": {"type": "string"},
						"tags": {"type": "array", "items": {"type": "string"}}
					},
					"required": ["title", "content"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "upload_image",
				Description: "Sube imagen a Google Drive y extrae texto con OCR",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"image_base64": {"type": "string"},
						"filename": {"type": "string"},
						"folder": {"type": "string", "enum": ["apuntes", "tareas", "documentos"]},
						"extract_text": {"type": "boolean", "default": true}
					},
					"required": ["image_base64", "filename"]
				}`),
			},
		},
	}
}

// ToOpenAITools convierte las definiciones al formato del SDK de OpenAI
func ToOpenAITools() []openai.ChatCompletionToolParam {
	defs := AllTools()
	result := make([]openai.ChatCompletionToolParam, len(defs))
	for i, d := range defs {
		var params shared.FunctionParameters
		_ = json.Unmarshal(d.Function.Parameters, &params)
		result[i] = openai.ChatCompletionToolParam{
			Type: constant.Function("function"),
			Function: shared.FunctionDefinitionParam{
				Name:        d.Function.Name,
				Description: openai.String(d.Function.Description),
				Parameters:  params,
			},
		}
	}
	return result
}
