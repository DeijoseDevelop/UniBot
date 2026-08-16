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
				Description: "Lista los cursos de Google Classroom del usuario (nombre, id y sección). year es opcional: si se indica (ej. 2026), filtra los cursos cuyo año esté en la sección/nombre",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"year": {"type": "integer", "description": "Año (opcional). Si se indica, solo cursos de ese año según su sección o nombre"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "list_classroom_tasks",
				Description: "Lista las tareas de Google Classroom. Por defecto excluye las vencidas. Filtros opcionales: year (año de la fecha límite), include_overdue (true para incluir vencidas), course_id (un curso específico)",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"course_id": {"type": "string", "description": "ID del curso (opcional). Si se omite, consulta todos los cursos"},
						"year": {"type": "integer", "description": "Año de la fecha límite (opcional)"},
						"include_overdue": {"type": "boolean", "default": false, "description": "Si es false (por defecto) excluye tareas vencidas; si es true las incluye"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "list_calendar_events",
				Description: "Lista eventos del Google Calendar del usuario en un rango de fechas. Por defecto desde hoy hacia adelante",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"start_date": {"type": "string", "format": "date-time", "description": "Inicio del rango (RFC3339). Opcional: hoy por defecto"},
						"end_date": {"type": "string", "format": "date-time", "description": "Fin del rango (RFC3339). Opcional"},
						"max_results": {"type": "integer", "default": 10, "description": "Máximo de eventos (opcional, por defecto 10)"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "update_calendar_event",
				Description: "Edita un evento existente del Google Calendar (título, fecha, duración o descripción). Solo requiere event_id; los demás campos son opcionales",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"event_id": {"type": "string", "description": "ID del evento a editar"},
						"title": {"type": "string", "description": "Nuevo título (opcional)"},
						"date": {"type": "string", "format": "date-time", "description": "Nueva fecha/hora de inicio (RFC3339, opcional)"},
						"duration_minutes": {"type": "integer", "description": "Nueva duración en minutos (opcional)"},
						"description": {"type": "string", "description": "Nueva descripción (opcional)"}
					},
					"required": ["event_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "delete_calendar_event",
				Description: "Elimina un evento del Google Calendar del usuario",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"event_id": {"type": "string", "description": "ID del evento a eliminar"}
					},
					"required": ["event_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_classroom_task",
				Description: "Obtiene los detalles completos de una tarea de Google Classroom: descripción, estado, fecha límite, puntos, tema, materiales adjuntos y estado de entrega. Requiere task_id (y course_id si se conoce)",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"task_id": {"type": "string", "description": "ID de la tarea (task_id devuelto por list_classroom_tasks)"},
						"course_id": {"type": "string", "description": "ID del curso (opcional si el task_id es globalmente único)"}
					},
					"required": ["task_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_calendar_event",
				Description: "Obtiene los detalles completos de un evento del Google Calendar: descripción, ubicación, asistentes, estado. Requiere event_id",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"event_id": {"type": "string", "description": "ID del evento (event_id devuelto por list_calendar_events)"}
					},
					"required": ["event_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_drive_file",
				Description: "Obtiene los detalles de un archivo de Google Drive: nombre, tipo, tamaño, fechas, enlace. Si include_content es true, devuelve también el texto del documento (solo Google Docs/Sheets/Slides)",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"file_id": {"type": "string", "description": "ID del archivo (id devuelto por search_drive_files o upload_image)"},
						"include_content": {"type": "boolean", "default": false, "description": "Si true, extrae el texto del documento (Google Docs/Sheets/Slides)"}
					},
					"required": ["file_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_note",
				Description: "Obtiene el contenido completo de una nota guardada en Notion (título, contenido y tags). Requiere note_id",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"note_id": {"type": "string", "description": "ID de la nota (id devuelto por query_notes)"}
					},
					"required": ["note_id"]
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "search_drive_files",
				Description: "Busca archivos en Google Drive del usuario por nombre. Filtros opcionales: query (texto en el nombre), folder (apuntes, tareas o documentos)",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Texto a buscar en el nombre del archivo (opcional)"},
						"folder": {"type": "string", "enum": ["apuntes", "tareas", "documentos"], "description": "Carpeta (opcional)"},
						"max_results": {"type": "integer", "default": 10, "description": "Máximo de archivos (opcional, por defecto 10)"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "query_notes",
				Description: "Busca notas guardadas en Notion por texto en el título. query es opcional: si se omite, devuelve las notas más recientes",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Texto a buscar en el título de las notas (opcional)"},
						"max_results": {"type": "integer", "default": 10, "description": "Máximo de notas (opcional, por defecto 10)"}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_weekly_summary",
				Description: "Genera un resumen de los próximos 7 días: eventos del calendario, tareas de Classroom que vencen en la semana y notas recientes de Notion",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {}
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
