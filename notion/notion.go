package notion

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jomei/notionapi"
)

const defaultDatabaseTitle = "UniBot Notes"

// Note representa una nota estructurada a guardar.
type Note struct {
	Title   string
	Content string
	Tags    []string
}

// Service encapsula el cliente de Notion.
type Service struct {
	client *notionapi.Client
	dbID   string
	anchor string
}

// New crea el servicio de Notion con el token de integración.
// databaseID y anchorPageID pueden estar vacíos: el servicio busca la DB por
// título o la crea automáticamente bajo la página ancla.
func New(token, databaseID, anchorPageID string) *Service {
	return &Service{
		client: notionapi.NewClient(notionapi.Token(token)),
		dbID:   databaseID,
		anchor: anchorPageID,
	}
}

// Save guarda una nota en la base de datos de Notion y devuelve su URL.
// Enfoque híbrido: usa la DB configurada; si no existe, la busca por título
// ("UniBot Notes") o la crea automáticamente bajo la página ancla.
func (s *Service) Save(ctx context.Context, note Note) (string, error) {
	databaseID, err := s.resolveDatabase(ctx)
	if err != nil {
		return "", err
	}

	properties, err := s.buildProperties(ctx, databaseID, note)
	if err != nil {
		return "", err
	}

	children := []notionapi.Block{}
	if strings.TrimSpace(note.Content) != "" {
		children = append(children, &notionapi.ParagraphBlock{
			BasicBlock: notionapi.BasicBlock{
				Object: notionapi.ObjectTypeBlock,
				Type:   notionapi.BlockTypeParagraph,
			},
			Paragraph: notionapi.Paragraph{
				RichText: []notionapi.RichText{richText(note.Content)},
			},
		})
	}

	page, err := s.client.Page.Create(ctx, &notionapi.PageCreateRequest{
		Parent:     notionapi.Parent{DatabaseID: databaseID},
		Properties: properties,
		Children:   children,
	})
	if err != nil {
		return "", fmt.Errorf("notion: crear página: %w", err)
	}

	return page.URL, nil
}

// resolveDatabase devuelve la DB a usar, validando/buscando/creando según sea
// necesario.
func (s *Service) resolveDatabase(ctx context.Context) (notionapi.DatabaseID, error) {
	if s.dbID != "" {
		id := notionapi.DatabaseID(s.dbID)
		if _, err := s.client.Database.Get(ctx, id); err == nil {
			return id, nil
		}
	}

	// Buscar una DB llamada "UniBot Notes"
	results, err := s.client.Search.Do(ctx, &notionapi.SearchRequest{
		Query:  defaultDatabaseTitle,
		Filter: notionapi.SearchFilter{Value: "database", Property: "object"},
	})
	if err != nil {
		return "", fmt.Errorf("notion: búsqueda de base de datos: %w", err)
	}
	for _, obj := range results.Results {
		if db, ok := obj.(*notionapi.Database); ok && dbTitle(db) == defaultDatabaseTitle {
			return notionapi.DatabaseID(db.ID), nil
		}
	}

	// Crearla automáticamente bajo la página ancla
	if s.anchor == "" {
		return "", errors.New("notion: no hay NOTION_DB_ID ni página ancla configurada")
	}
	return s.createDatabase(ctx)
}

func (s *Service) createDatabase(ctx context.Context) (notionapi.DatabaseID, error) {
	parent := notionapi.Parent{
		Type:   notionapi.ParentTypePageID,
		PageID: notionapi.PageID(s.anchor),
	}
	db, err := s.client.Database.Create(ctx, &notionapi.DatabaseCreateRequest{
		Parent: parent,
		Title:  []notionapi.RichText{richText(defaultDatabaseTitle)},
		Properties: notionapi.PropertyConfigs{
			"title":   &notionapi.TitlePropertyConfig{Type: notionapi.PropertyConfigTypeTitle},
			"content": &notionapi.RichTextPropertyConfig{Type: notionapi.PropertyConfigTypeRichText},
			"tags":    &notionapi.MultiSelectPropertyConfig{Type: notionapi.PropertyConfigTypeMultiSelect},
		},
		IsInline: true,
	})
	if err != nil {
		return "", fmt.Errorf("notion: crear base de datos: %w", err)
	}
	return notionapi.DatabaseID(db.ID), nil
}

// buildProperties construye las propiedades de la página adaptándose al esquema
// real de la base de datos (title / rich_text / multi_select por tipo).
func (s *Service) buildProperties(ctx context.Context, databaseID notionapi.DatabaseID, note Note) (notionapi.Properties, error) {
	db, err := s.client.Database.Get(ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("notion: leer base de datos: %w", err)
	}

	var titleKey, richTextKey, multiSelectKey string
	for name, cfg := range db.Properties {
		switch cfg.GetType() {
		case notionapi.PropertyConfigTypeTitle:
			if titleKey == "" {
				titleKey = name
			}
		case notionapi.PropertyConfigTypeRichText:
			if richTextKey == "" {
				richTextKey = name
			}
		case notionapi.PropertyConfigTypeMultiSelect:
			if multiSelectKey == "" {
				multiSelectKey = name
			}
		}
	}

	if titleKey == "" {
		return nil, errors.New("notion: la base de datos no tiene propiedad de título")
	}

	props := notionapi.Properties{
		titleKey: &notionapi.TitleProperty{
			Title: []notionapi.RichText{richText(note.Title)},
		},
	}

	if richTextKey != "" {
		props[richTextKey] = &notionapi.RichTextProperty{
			RichText: []notionapi.RichText{richText(note.Content)},
		}
	}

	if multiSelectKey != "" && len(note.Tags) > 0 {
		options := make([]notionapi.Option, 0, len(note.Tags))
		for _, t := range note.Tags {
			options = append(options, notionapi.Option{Name: t})
		}
		props[multiSelectKey] = &notionapi.MultiSelectProperty{
			MultiSelect: options,
		}
	}

	return props, nil
}

// NoteRef es una referencia a una nota guardada (para consulta).
type NoteRef struct {
	ID      string   `json:"id,omitempty"`
	Title   string   `json:"title"`
	URL     string   `json:"url"`
	Tags    []string `json:"tags,omitempty"`
	Content string   `json:"content,omitempty"`
}

// Search consulta las notas de la base de datos, filtrando por texto en el
// título. Devuelve hasta limit notas.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]NoteRef, error) {
	databaseID, err := s.resolveDatabase(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Database.Query(ctx, databaseID, &notionapi.DatabaseQueryRequest{
		PageSize: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("notion: consultar base de datos: %w", err)
	}

	query = strings.ToLower(strings.TrimSpace(query))
	notes := []NoteRef{}
	for _, page := range resp.Results {
		title, content, tags := pageNote(&page)
		if query != "" && !strings.Contains(strings.ToLower(title), query) {
			continue
		}
		notes = append(notes, NoteRef{
			ID:      string(page.ID),
			Title:   title,
			URL:     page.URL,
			Tags:    tags,
			Content: content,
		})
	}
	return notes, nil
}

// Get obtiene el contenido completo de una nota por su ID de página,
// incluyendo el texto de sus bloques.
func (s *Service) Get(ctx context.Context, noteID string) (NoteRef, error) {
	page, err := s.client.Page.Get(ctx, notionapi.PageID(noteID))
	if err != nil {
		return NoteRef{}, fmt.Errorf("notion: leer página: %w", err)
	}

	title, content, tags := pageNote(page)

	// Concatenar el texto de los bloques de la página
	resp, err := s.client.Block.GetChildren(ctx, notionapi.BlockID(noteID), nil)
	if err == nil {
		parts := []string{}
		for _, b := range resp.Results {
			if text := b.GetRichTextString(); text != "" && !strings.Contains(text, "No rich text of a basic block") {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			content = strings.Join(parts, "\n")
		}
	}

	return NoteRef{
		ID:      noteID,
		Title:   title,
		URL:     page.URL,
		Tags:    tags,
		Content: content,
	}, nil
}

// pageNote extrae título, contenido y tags de una página de la base de notas.
func pageNote(page *notionapi.Page) (title, content string, tags []string) {
	for _, prop := range page.Properties {
		switch p := prop.(type) {
		case *notionapi.TitleProperty:
			title = richTextPlain(p.Title)
		case *notionapi.RichTextProperty:
			content = richTextPlain(p.RichText)
		case *notionapi.MultiSelectProperty:
			for _, o := range p.MultiSelect {
				tags = append(tags, o.Name)
			}
		}
	}
	return
}

func richTextPlain(rt []notionapi.RichText) string {
	var sb strings.Builder
	for _, r := range rt {
		sb.WriteString(r.PlainText)
	}
	return sb.String()
}

func richText(text string) notionapi.RichText {
	return notionapi.RichText{
		Text: &notionapi.Text{Content: text},
	}
}

func dbTitle(db *notionapi.Database) string {
	if len(db.Title) == 0 {
		return ""
	}
	return db.Title[0].PlainText
}
