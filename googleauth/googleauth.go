package googleauth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/classroom/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/vision/v1"
)

// TokenStore define la interfaz para persistir tokens
type TokenStore interface {
	GetTokens(ctx context.Context, userID int64) (*oauth2.Token, error)
	SaveTokens(ctx context.Context, userID int64, token *oauth2.Token) error
}

// AutoRefreshTokenSource implementa oauth2.TokenSource con refresh automático
type AutoRefreshTokenSource struct {
	userID       int64
	config       *oauth2.Config
	store        TokenStore
	currentToken *oauth2.Token
}

// scopes devuelve los scopes mínimos necesarios de Google.
func scopes() []string {
	return []string{
		calendar.CalendarScope,
		classroom.ClassroomCoursesReadonlyScope,
		classroom.ClassroomCourseworkMeReadonlyScope,
		drive.DriveFileScope,
		vision.CloudVisionScope,
	}
}

// NewOAuthConfig construye la configuración OAuth2 para el flujo de
// autorización (comando /auth).
func NewOAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     google.Endpoint,
		Scopes:       scopes(),
	}
}

// NewAutoRefreshTokenSource crea un TokenSource que refresca automáticamente
func NewAutoRefreshTokenSource(userID int64, clientID, clientSecret string, store TokenStore) *AutoRefreshTokenSource {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       scopes(),
	}
	return &AutoRefreshTokenSource{
		userID: userID,
		config: config,
		store:  store,
	}
}

// AuthURL genera la URL de autorización OAuth2 para el usuario.
func (a *AutoRefreshTokenSource) AuthURL(state string) string {
	return a.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Token implementa oauth2.TokenSource
func (a *AutoRefreshTokenSource) Token() (*oauth2.Token, error) {
	if a.currentToken == nil {
		tok, err := a.store.GetTokens(context.Background(), a.userID)
		if err != nil {
			return nil, fmt.Errorf("token no encontrado: %w", err)
		}
		a.currentToken = tok
	}

	// Si el token expiró o expira en menos de 60 segundos, refrescar
	if a.currentToken.Expiry.Before(time.Now().Add(60 * time.Second)) {
		newToken, err := a.config.TokenSource(context.Background(), a.currentToken).Token()
		if err != nil {
			return nil, fmt.Errorf("refresh token failed: %w", err)
		}
		a.currentToken = newToken
		if err := a.store.SaveTokens(context.Background(), a.userID, newToken); err != nil {
			return nil, fmt.Errorf("save refreshed token failed: %w", err)
		}
	}

	return a.currentToken, nil
}

// GetCalendarService devuelve un servicio de Calendar autenticado
func (a *AutoRefreshTokenSource) GetCalendarService(ctx context.Context) (*calendar.Service, error) {
	client := oauth2.NewClient(ctx, a)
	return calendar.NewService(ctx, option.WithHTTPClient(client))
}

// GetClassroomService devuelve un servicio de Classroom autenticado
func (a *AutoRefreshTokenSource) GetClassroomService(ctx context.Context) (*classroom.Service, error) {
	client := oauth2.NewClient(ctx, a)
	return classroom.NewService(ctx, option.WithHTTPClient(client))
}

// GetDriveService devuelve un servicio de Drive autenticado
func (a *AutoRefreshTokenSource) GetDriveService(ctx context.Context) (*drive.Service, error) {
	client := oauth2.NewClient(ctx, a)
	return drive.NewService(ctx, option.WithHTTPClient(client))
}

// GetVisionService devuelve un servicio de Vision autenticado
func (a *AutoRefreshTokenSource) GetVisionService(ctx context.Context) (*vision.Service, error) {
	client := oauth2.NewClient(ctx, a)
	return vision.NewService(ctx, option.WithHTTPClient(client))
}
