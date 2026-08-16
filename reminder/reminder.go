package reminder

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"unibot/config"
	"unibot/googleauth"
	"unibot/store"
)

// Sender permite enviar mensajes a un usuario de Telegram.
type Sender interface {
	SendToUser(ctx context.Context, userID int64, text string) error
}

// Service revisa periódicamente las tareas de Classroom y los eventos del
// calendario de los usuarios conectados, y les avisa por Telegram.
type Service struct {
	store    *store.TokenStore
	sender   Sender
	notified map[int64]map[string]bool // userID -> clave (fecha:item) ya notificada
	mu       sync.Mutex
}

func New(tokenStore *store.TokenStore, sender Sender) *Service {
	return &Service{
		store:    tokenStore,
		sender:   sender,
		notified: map[int64]map[string]bool{},
	}
}

// Run ejecuta el ciclo de recordatorios cada 60 minutos hasta que el contexto
// se cancele.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Minute)
	defer ticker.Stop()

	s.check(ctx) // ejecutar al arrancar
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *Service) check(ctx context.Context) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		log.Printf("[reminder] list users: %v", err)
		return
	}

	now := time.Now().In(bogotaTZ())
	horizon := now.Add(time.Duration(config.Cfg.ReminderHoursAhead) * time.Hour)
	todayKey := now.Format("2006-01-02")

	for _, userID := range users {
		lines, err := s.buildReminders(ctx, userID, now, horizon)
		if err != nil {
			log.Printf("[reminder] user %d: %v", userID, err)
			continue
		}
		if len(lines) == 0 {
			continue
		}

		s.mu.Lock()
		if s.notified[userID] == nil {
			s.notified[userID] = map[string]bool{}
		}
		// Purgar claves de días anteriores
		for k := range s.notified[userID] {
			if !strings.HasPrefix(k, todayKey) {
				delete(s.notified[userID], k)
			}
		}
		s.mu.Unlock()

		msg := []string{"🔔 Recordatorios de hoy:"}
		var pending []string
		for _, l := range lines {
			key := todayKey + ":" + l.key
			s.mu.Lock()
			already := s.notified[userID][key]
			if !already {
				s.notified[userID][key] = true
			}
			s.mu.Unlock()
			if !already {
				pending = append(pending, l.text)
			}
		}
		if len(pending) == 0 {
			continue
		}
		msg = append(msg, pending...)
		if err := s.sender.SendToUser(ctx, userID, strings.Join(msg, "\n")); err != nil {
			log.Printf("[reminder] send to %d: %v", userID, err)
		}
	}
}

type reminderLine struct {
	key  string
	text string
}

func (s *Service) buildReminders(ctx context.Context, userID int64, now, horizon time.Time) ([]reminderLine, error) {
	lines := []reminderLine{}

	ts := googleauth.NewAutoRefreshTokenSource(userID, config.Cfg.GoogleClientID, config.Cfg.GoogleClientSecret, s.store)

	// Tareas de Classroom que vencen dentro del horizonte
	classroomSvc, err := ts.GetClassroomService(ctx)
	if err == nil {
		courses, err := classroomSvc.Courses.List().StudentId("me").Context(ctx).Do()
		if err == nil {
			for _, course := range courses.Courses {
				work, err := classroomSvc.Courses.CourseWork.List(course.Id).Context(ctx).Do()
				if err != nil {
					continue
				}
				for _, w := range work.CourseWork {
					if w.DueDate == nil {
						continue
					}
					due := time.Date(
						int(w.DueDate.Year), time.Month(w.DueDate.Month), int(w.DueDate.Day),
						23, 59, 59, 0, bogotaTZ(),
					)
					if due.After(now) && !due.After(horizon) {
						lines = append(lines, reminderLine{
							key:  "task:" + w.Id,
							text: fmt.Sprintf("📚 %s — vence %d/%d", w.Title, w.DueDate.Day, w.DueDate.Month),
						})
					}
				}
			}
		}
	}

	// Eventos del calendario que empiezan dentro del horizonte
	calSvc, err := ts.GetCalendarService(ctx)
	if err == nil {
		events, err := calSvc.Events.List("primary").
			SingleEvents(true).
			OrderBy("startTime").
			TimeMin(now.Format(time.RFC3339)).
			TimeMax(horizon.Format(time.RFC3339)).
			MaxResults(20).
			Context(ctx).Do()
		if err == nil {
			for _, ev := range events.Items {
				if ev.Start == nil {
					continue
				}
				startStr := ev.Start.DateTime
				if startStr == "" {
					continue // eventos de día completo: se omiten
				}
				start, err := time.Parse(time.RFC3339, startStr)
				if err != nil {
					continue
				}
				lines = append(lines, reminderLine{
					key:  "event:" + ev.Id,
					text: fmt.Sprintf("📅 %s — %s", ev.Summary, start.In(bogotaTZ()).Format("15:04")),
				})
			}
		}
	}

	return lines, nil
}

func bogotaTZ() *time.Location {
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		return time.UTC
	}
	return loc
}
