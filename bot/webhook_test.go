package bot

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// TestWebhookDispatch verifica que StartWebhookMode consume las updates del
// canal y despacha al handler registrado.
func TestWebhookDispatch(t *testing.T) {
	var handled bool

	// Handler mínimo que marca que fue invocado
	mark := bot.HandlerFunc(func(ctx context.Context, b *bot.Bot, update *models.Update) {
		handled = true
	})

	instance, err := bot.New("test-token", bot.WithSkipGetMe(), bot.WithDefaultHandler(mark))
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go instance.StartWebhook(ctx)

	handler := instance.WebhookHandler()

	payload := `{"update_id":1,"message":{"message_id":1,"from":{"id":1,"is_bot":false,"first_name":"T"},"chat":{"id":1,"type":"private","first_name":"T"},"date":1786900000,"text":"/start"}}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	handler(rec, req)

	time.Sleep(200 * time.Millisecond) // dispatch es asíncrono

	if !handled {
		t.Fatal("el handler NO fue invocado: el dispatcher no consume las updates")
	}
	t.Log("OK: update despachada al handler")
}
