package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCmdStatus_ReportsAPIStartupFailure(t *testing.T) {
	for _, lang := range []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish} {
		for _, cards := range []bool{false, true} {
			name := string(lang) + "/text"
			if cards {
				name = string(lang) + "/card"
			}
			t.Run(name, func(t *testing.T) {
				plain := &stubPlatformEngine{n: "plain"}
				card := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "cards"}}
				var p Platform = plain
				if cards {
					p = card
				}
				e := NewEngine("test", &stubAgent{}, []Platform{p}, "", lang)
				msg := &Message{SessionKey: "test:user1", ReplyCtx: "ctx"}
				reason := "lock API socket: already held by another instance"
				for _, err := range []error{nil, errors.New(reason), nil} {
					e.SetAPIStartupError(err)
					e.cmdStatus(p, msg)
					var text string
					if cards {
						latest := card.repliedCards[len(card.repliedCards)-1]
						text = latest.RenderText()
						wantColor := "green"
						if err != nil {
							wantColor = "orange"
						}
						if latest.Header.Color != wantColor {
							t.Fatalf("status color = %q, want %q", latest.Header.Color, wantColor)
						}
					} else {
						sent := plain.getSent()
						text = sent[len(sent)-1]
					}
					if err == nil {
						if strings.Contains(text, reason) || strings.Contains(text, "cc-connect send") {
							t.Fatalf("healthy status retained API failure: %s", text)
						}
					} else {
						warning := e.i18n.Tf(MsgStatusAPIUnavailable, reason)
						if !strings.Contains(text, warning) || !strings.Contains(text, reason) || !strings.Contains(text, "cc-connect send") {
							t.Fatalf("status omitted API failure and impact: %s", text)
						}
					}
				}
			})
		}
	}
}

func TestMgmt_StatusReportsAPIStartupFailure(t *testing.T) {
	// Degradation must be visible even with no projects registered.
	m := NewManagementServer(0, "", nil)
	reason := "API socket path is not a socket: /data/run/api.sock"
	for _, err := range []error{nil, errors.New(reason), nil} {
		m.SetAPIStartupError(err)
		w := httptest.NewRecorder()
		m.handleStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
		var response struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		got, present := response.Data["api_startup_error"]
		if err == nil {
			if present {
				t.Fatalf("healthy status retained api_startup_error: %v", got)
			}
		} else if got != reason {
			t.Fatalf("api_startup_error = %v, want %q", got, reason)
		}
	}
}

func TestAPIStartupStatusConcurrentUpdates(t *testing.T) {
	e := NewEngine("test", &stubAgent{}, nil, "", LangEnglish)
	m := NewManagementServer(0, "", nil)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			for _, err := range []error{errors.New("socket busy"), nil} {
				e.SetAPIStartupError(err)
				m.SetAPIStartupError(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_ = e.apiStartupWarning()
			m.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
		}
	}()
	wg.Wait()
}
