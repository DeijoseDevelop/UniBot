package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"unibot/config"
	"unibot/googleauth"
	"unibot/notion"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/vision/v1"
)

// Executor ejecuta las herramientas invocadas por el LLM
type Executor struct {
	tokenStore googleauth.TokenStore
	notion     *notion.Service
}

func New(store googleauth.TokenStore, notionService *notion.Service) *Executor {
	return &Executor{tokenStore: store, notion: notionService}
}

// Execute despacha la ejecución según el nombre de la tool
func (e *Executor) Execute(ctx context.Context, userID int64, name string, args json.RawMessage) (map[string]interface{}, error) {
	switch name {
	case "create_calendar_event":
		return e.createCalendarEvent(ctx, userID, args)
	case "list_classroom_tasks":
		return e.listClassroomTasks(ctx, userID, args)
	case "save_note":
		return e.saveNote(ctx, userID, args)
	case "upload_image":
		return e.uploadImage(ctx, userID, args)
	default:
		return nil, fmt.Errorf("tool %s no implementada", name)
	}
}

func (e *Executor) createCalendarEvent(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		Title           string `json:"title"`
		Date            string `json:"date"`
		Description     string `json:"description"`
		DurationMinutes int    `json:"duration_minutes"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.DurationMinutes == 0 {
		params.DurationMinutes = 60
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetCalendarService(ctx)
	if err != nil {
		return nil, err
	}

	start, err := time.Parse(time.RFC3339, params.Date)
	if err != nil {
		return nil, fmt.Errorf("fecha inválida: %w", err)
	}
	end := start.Add(time.Duration(params.DurationMinutes) * time.Minute)

	event := &calendar.Event{
		Summary:     params.Title,
		Description: params.Description,
		Start:       &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: "America/Bogota"},
		End:         &calendar.EventDateTime{DateTime: end.Format(time.RFC3339), TimeZone: "America/Bogota"},
	}

	result, err := svc.Events.Insert("primary", event).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar API error: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"event_id": result.Id,
		"link":     result.HtmlLink,
		"summary":  fmt.Sprintf("Evento creado: %s", params.Title),
	}, nil
}

func (e *Executor) listClassroomTasks(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetClassroomService(ctx)
	if err != nil {
		return nil, err
	}

	courses, err := svc.Courses.List().StudentId("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("classroom API error: %w", err)
	}

	tasks := []map[string]string{}
	for _, course := range courses.Courses {
		work, err := svc.Courses.CourseWork.List(course.Id).Context(ctx).Do()
		if err != nil {
			continue
		}
		for _, w := range work.CourseWork {
			due := "Sin fecha"
			if w.DueDate != nil {
				due = fmt.Sprintf("%d/%d/%d", w.DueDate.Day, w.DueDate.Month, w.DueDate.Year)
			}
			tasks = append(tasks, map[string]string{
				"course": course.Name,
				"title":  w.Title,
				"due":    due,
				"link":   w.AlternateLink,
			})
		}
	}

	return map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	}, nil
}

func (e *Executor) saveNote(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if e.notion == nil {
		return nil, errors.New("notion no configurado: NOTION_TOKEN requerido")
	}

	url, err := e.notion.Save(ctx, notion.Note{
		Title:   params.Title,
		Content: params.Content,
		Tags:    params.Tags,
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success":  true,
		"saved_to": "Notion",
		"link":     url,
		"summary":  fmt.Sprintf("Nota guardada en Notion: %s", params.Title),
	}, nil
}

func (e *Executor) uploadImage(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		ImageBase64 string `json:"image_base64"`
		Filename    string `json:"filename"`
		Folder      string `json:"folder"`
		ExtractText bool   `json:"extract_text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetDriveService(ctx)
	if err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(params.ImageBase64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %w", err)
	}

	file := &drive.File{
		Name:    params.Filename,
		Parents: []string{getFolderID(params.Folder)},
	}

	result, err := svc.Files.Create(file).Media(bytes.NewReader(data)).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("drive API error: %w", err)
	}

	extractedText := ""
	if params.ExtractText {
		visionSvc, err := ts.GetVisionService(ctx)
		if err == nil {
			req := &vision.AnnotateImageRequest{
				Image:    &vision.Image{Content: params.ImageBase64},
				Features: []*vision.Feature{{Type: "TEXT_DETECTION"}},
			}
			resp, err := visionSvc.Images.Annotate(&vision.BatchAnnotateImagesRequest{Requests: []*vision.AnnotateImageRequest{req}}).Context(ctx).Do()
			if err == nil && len(resp.Responses) > 0 && resp.Responses[0].FullTextAnnotation != nil {
				extractedText = resp.Responses[0].FullTextAnnotation.Text
			}
		}
	}

	return map[string]interface{}{
		"success":        true,
		"file_id":        result.Id,
		"link":           fmt.Sprintf("https://drive.google.com/file/d/%s/view", result.Id),
		"extracted_text": extractedText,
	}, nil
}

func getFolderID(folder string) string {
	// Mapeo de carpetas a IDs de Drive (configurables)
	folders := map[string]string{
		"apuntes":    "FOLDER_APUNTES_ID",
		"tareas":     "FOLDER_TAREAS_ID",
		"documentos": "FOLDER_DOCUMENTOS_ID",
	}
	if id, ok := folders[folder]; ok {
		return id
	}
	return "root"
}
