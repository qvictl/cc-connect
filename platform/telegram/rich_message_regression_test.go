package telegram

import (
	"context"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestRichMessageResultRejectsIncompleteSuccess(t *testing.T) {
	for _, msg := range []*models.Message{nil, {}} {
		if _, err := validateRichMessageResult(msg); err == nil {
			t.Fatal("incomplete rich response accepted")
		}
	}
	msg := &models.Message{ID: 7}
	if got, err := validateRichMessageResult(msg); err != nil || got != msg {
		t.Fatal("valid rich response rejected")
	}
}

// A disconnected platform must fail before constructing or sending a request.
// No credentials, test server, or HTTP client are needed for this regression.
func TestRichMessageDisconnectedDoesNotRequest(t *testing.T) {
	p := &Platform{}
	if _, err := p.sendRichMessage(context.Background(), 1, 0, 0, "hello", nil); err == nil {
		t.Fatal("disconnected rich send accepted")
	}
	if _, err := p.editRichMessage(context.Background(), 1, 1, "hello", nil); err == nil {
		t.Fatal("disconnected rich edit accepted")
	}
}
