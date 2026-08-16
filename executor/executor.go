package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"unibot/config"
	"unibot/googleauth"
	"unibot/notion"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/vision/v1"
)

var bogotaTZ = func() *time.Location {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		return time.UTC
	}
	return loc
}()

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
	case "list_calendar_events":
		return e.listCalendarEvents(ctx, userID, args)
	case "update_calendar_event":
		return e.updateCalendarEvent(ctx, userID, args)
	case "delete_calendar_event":
		return e.deleteCalendarEvent(ctx, userID, args)
	case "list_classroom_courses":
		return e.listClassroomCourses(ctx, userID, args)
	case "list_classroom_tasks":
		return e.listClassroomTasks(ctx, userID, args)
	case "save_note":
		return e.saveNote(ctx, userID, args)
	case "query_notes":
		return e.queryNotes(ctx, userID, args)
	case "upload_image":
		return e.uploadImage(ctx, userID, args)
	case "search_drive_files":
		return e.searchDriveFiles(ctx, userID, args)
	default:
		return nil, fmt.Errorf("tool %s no implementada", name)
	}
}

func (e *Executor) listClassroomCourses(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		Year int `json:"year"`
	}
	_ = json.Unmarshal(args, &params)

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetClassroomService(ctx)
	if err != nil {
		return nil, err
	}

	courses, err := svc.Courses.List().StudentId("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("classroom API error: %w", err)
	}

	yearRE := regexp.MustCompile(`20\d{2}`)
	result := []map[string]interface{}{}
	for _, course := range courses.Courses {
		// Filtro por año: se busca en la sección o el nombre del curso
		if params.Year > 0 {
			match := yearRE.FindString(course.Section)
			if match == "" {
				match = yearRE.FindString(course.Name)
			}
			if match != "" && match != fmt.Sprintf("%d", params.Year) {
				continue
			}
		}
		result = append(result, map[string]interface{}{
			"id":      course.Id,
			"name":    course.Name,
			"section": course.Section,
		})
	}

	return map[string]interface{}{
		"courses": result,
		"count":   len(result),
	}, nil
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
	var params struct {
		CourseID       string `json:"course_id"`
		Year           int    `json:"year"`
		IncludeOverdue bool   `json:"include_overdue"`
	}
	_ = json.Unmarshal(args, &params)

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetClassroomService(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(bogotaTZ)
	filtered := 0

	tasks := []map[string]string{}
	appendCourse := func(courseID, courseName string) error {
		work, err := svc.Courses.CourseWork.List(courseID).Context(ctx).Do()
		if err != nil {
			return err
		}
		for _, w := range work.CourseWork {
			if w.DueDate == nil {
				// Sin fecha: se conserva salvo que se pida filtrar por año
				if params.Year > 0 {
					filtered++
					continue
				}
			} else {
				due := time.Date(
					int(w.DueDate.Year), time.Month(w.DueDate.Month), int(w.DueDate.Day),
					23, 59, 59, 0, bogotaTZ,
				)
				// Excluir vencidas (comportamiento por defecto)
				if !params.IncludeOverdue && due.Before(now) {
					filtered++
					continue
				}
				// Filtrar por año de la fecha límite
				if params.Year > 0 && due.Year() != params.Year {
					filtered++
					continue
				}
			}
			due := "Sin fecha"
			if w.DueDate != nil {
				due = fmt.Sprintf("%d/%d/%d", w.DueDate.Day, w.DueDate.Month, w.DueDate.Year)
			}
			tasks = append(tasks, map[string]string{
				"course": courseName,
				"title":  w.Title,
				"due":    due,
				"link":   w.AlternateLink,
			})
		}
		return nil
	}

	if params.CourseID != "" {
		course, err := svc.Courses.Get(params.CourseID).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("classroom API error: %w", err)
		}
		if err := appendCourse(course.Id, course.Name); err != nil {
			return nil, fmt.Errorf("classroom API error: %w", err)
		}
	} else {
		courses, err := svc.Courses.List().StudentId("me").Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("classroom API error: %w", err)
		}
		for _, course := range courses.Courses {
			if err := appendCourse(course.Id, course.Name); err != nil {
				continue
			}
		}
	}

	result := map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	}
	if filtered > 0 {
		result["filtered_count"] = filtered
	}
	return result, nil
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

func (e *Executor) queryNotes(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	_ = json.Unmarshal(args, &params)
	if params.MaxResults <= 0 || params.MaxResults > 50 {
		params.MaxResults = 10
	}
	if e.notion == nil {
		return nil, errors.New("notion no configurado: NOTION_TOKEN requerido")
	}

	notes, err := e.notion.Search(ctx, params.Query, params.MaxResults)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"notes": notes,
		"count": len(notes),
	}, nil
}

func (e *Executor) listCalendarEvents(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		StartDate  string `json:"start_date"`
		EndDate    string `json:"end_date"`
		MaxResults int64  `json:"max_results"`
	}
	_ = json.Unmarshal(args, &params)
	if params.MaxResults <= 0 || params.MaxResults > 50 {
		params.MaxResults = 10
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetCalendarService(ctx)
	if err != nil {
		return nil, err
	}

	call := svc.Events.List("primary").
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(params.MaxResults)

	now := time.Now().In(bogotaTZ)
	if params.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, params.StartDate); err == nil {
			call = call.TimeMin(t.Format(time.RFC3339))
		}
	} else {
		call = call.TimeMin(now.Format(time.RFC3339))
	}
	if params.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, params.EndDate); err == nil {
			call = call.TimeMax(t.Format(time.RFC3339))
		}
	}

	events, err := call.Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar API error: %w", err)
	}

	result := []map[string]interface{}{}
	for _, ev := range events.Items {
		start := "Sin fecha"
		if ev.Start != nil {
			start = ev.Start.DateTime
			if start == "" {
				start = ev.Start.Date
			}
		}
		result = append(result, map[string]interface{}{
			"id":          ev.Id,
			"title":       ev.Summary,
			"description": ev.Description,
			"start":       start,
			"link":        ev.HtmlLink,
		})
	}

	return map[string]interface{}{
		"events": result,
		"count":  len(result),
	}, nil
}

func (e *Executor) updateCalendarEvent(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		EventID         string `json:"event_id"`
		Title           string `json:"title"`
		Date            string `json:"date"`
		DurationMinutes int    `json:"duration_minutes"`
		Description     string `json:"description"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.EventID == "" {
		return nil, errors.New("event_id requerido")
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetCalendarService(ctx)
	if err != nil {
		return nil, err
	}

	event, err := svc.Events.Get("primary", params.EventID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar API error: %w", err)
	}

	// Detectar cambios
	updates := []string{}
	if params.Title != "" && params.Title != event.Summary {
		event.Summary = params.Title
		updates = append(updates, "título")
	}
	if params.Description != "" && params.Description != event.Description {
		event.Description = params.Description
		updates = append(updates, "descripción")
	}
	if params.Date != "" {
		start, err := time.Parse(time.RFC3339, params.Date)
		if err != nil {
			return nil, fmt.Errorf("fecha inválida: %w", err)
		}
		duration := time.Duration(params.DurationMinutes) * time.Minute
		if params.DurationMinutes == 0 {
			// Conservar duración actual si existe
			if event.Start != nil && event.End != nil {
				if s, err1 := time.Parse(time.RFC3339, event.Start.DateTime); err1 == nil {
					if e2, err2 := time.Parse(time.RFC3339, event.End.DateTime); err2 == nil {
						duration = e2.Sub(s)
					}
				}
			}
			if duration <= 0 {
				duration = 60 * time.Minute
			}
		}
		event.Start = &calendar.EventDateTime{DateTime: start.Format(time.RFC3339), TimeZone: "America/Bogota"}
		event.End = &calendar.EventDateTime{DateTime: start.Add(duration).Format(time.RFC3339), TimeZone: "America/Bogota"}
		updates = append(updates, "fecha")
	}
	if len(updates) == 0 {
		return map[string]interface{}{"success": true, "message": "Sin cambios detectados"}, nil
	}

	result, err := svc.Events.Update("primary", params.EventID, event).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar API error: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"event_id": result.Id,
		"updated":  updates,
		"link":     result.HtmlLink,
	}, nil
}

func (e *Executor) deleteCalendarEvent(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	if params.EventID == "" {
		return nil, errors.New("event_id requerido")
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetCalendarService(ctx)
	if err != nil {
		return nil, err
	}

	if err := svc.Events.Delete("primary", params.EventID).Context(ctx).Do(); err != nil {
		return nil, fmt.Errorf("calendar API error: %w", err)
	}

	return map[string]interface{}{
		"success":  true,
		"event_id": params.EventID,
		"message":  "Evento eliminado",
	}, nil
}

func (e *Executor) searchDriveFiles(ctx context.Context, userID int64, args json.RawMessage) (map[string]interface{}, error) {
	var params struct {
		Query      string `json:"query"`
		Folder     string `json:"folder"`
		MaxResults int64  `json:"max_results"`
	}
	_ = json.Unmarshal(args, &params)
	if params.MaxResults <= 0 || params.MaxResults > 50 {
		params.MaxResults = 10
	}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, e.tokenStore)
	svc, err := ts.GetDriveService(ctx)
	if err != nil {
		return nil, err
	}

	q := "trashed = false"
	if params.Query != "" {
		q += fmt.Sprintf(" and name contains '%s'", escapeQuery(params.Query))
	}
	if params.Folder != "" && getFolderID(params.Folder) != "root" {
		q += fmt.Sprintf(" and '%s' in parents", getFolderID(params.Folder))
	}

	files, err := svc.Files.List().
		Q(q).
		Fields("files(id,name,mimeType,webViewLink,createdTime)").
		PageSize(params.MaxResults).
		OrderBy("createdTime desc").
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("drive API error: %w", err)
	}

	result := []map[string]interface{}{}
	for _, f := range files.Files {
		result = append(result, map[string]interface{}{
			"id":        f.Id,
			"name":      f.Name,
			"mime_type": f.MimeType,
			"link":      f.WebViewLink,
		})
	}

	return map[string]interface{}{
		"files": result,
		"count": len(result),
	}, nil
}

func escapeQuery(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "'", "\\'"), "\\", "\\\\")
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
