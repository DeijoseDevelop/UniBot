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
