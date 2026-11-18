package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

var cardTaskIDPattern = regexp.MustCompile(`^cc_[0-9a-f]{32}$`)

func taskIDRegressionCard(title *string) *core.Card {
	builder := core.NewCard().Buttons(
		core.CardButton{Text: "Allow", Type: "primary", Value: "perm:allow", Extra: map[string]string{"perm_request_id": "prompt-current"}},
		core.DangerBtn("Deny", "opaque:key / 原样"),
	)
	if title != nil {
		builder.Title(*title, "blue")
	}
	return builder.Build()
}

func TestCardTaskIDHeaderAndButtonPreservation(t *testing.T) {
	for _, title := range []string{"Review request", "", " \t\n "} {
		t.Run(fmt.Sprintf("title_%q", title), func(t *testing.T) {
			payload, err := buildWeComTemplateCard(taskIDRegressionCard(&title))
			if err != nil {
				t.Fatal(err)
			}
			wantTitle := title
			if strings.TrimSpace(title) == "" {
				wantTitle = "cc-connect"
			}
			assertTaskIDRegressionPayload(t, payload, wantTitle)
		})
	}
	payload, err := buildWeComTemplateCard(taskIDRegressionCard(nil))
	if err != nil {
		t.Fatal(err)
	}
	assertTaskIDRegressionPayload(t, payload, "cc-connect")
}

// Normalize through JSON so this assertion checks both builder maps and actual envelopes.
func assertTaskIDRegressionPayload(t *testing.T, payload map[string]any, title string) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	id, ok := got["task_id"].(string)
	if !ok || len(id) != 35 || !cardTaskIDPattern.MatchString(id) {
		t.Fatal("interaction card has missing or malformed task_id")
	}
	if got["card_type"] != "button_interaction" || !reflect.DeepEqual(got["main_title"], map[string]any{"title": title}) {
		t.Fatalf("unexpected type/title: %v / %v", got["card_type"], got["main_title"])
	}
	wantButtons := []any{
		map[string]any{"text": "Allow", "style": float64(1), "key": "perm:prompt-current:allow"},
		map[string]any{"text": "Deny", "style": float64(3), "key": "opaque:key / 原样"},
	}
	if !reflect.DeepEqual(got["button_list"], wantButtons) {
		t.Fatal("button keys, labels or styles changed")
	}
	if !reflect.DeepEqual(got["card_action"], map[string]any{"type": float64(0)}) {
		t.Fatal("default card action changed")
	}
	return id
}

func TestCardTaskIDConcurrentUniqueness(t *testing.T) {
	const count = 256
	type result struct {
		payload map[string]any
		err     error
	}
	results := make(chan result, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := buildWeComTemplateCard(taskIDRegressionCard(nil))
			results <- result{payload, err}
		}()
	}
	wg.Wait()
	close(results)
	seen := make(map[string]bool, count)
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		id := assertTaskIDRegressionPayload(t, result.payload, "cc-connect")
		if seen[id] {
			t.Fatal("duplicate task_id across independently built cards")
		}
		seen[id] = true
	}
}

type taskIDErrorReader struct{ err error }

func (r taskIDErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestCardTaskIDRandomReadFailure(t *testing.T) {
	sentinel := errors.New("entropy source unavailable")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{"reader_error", taskIDErrorReader{sentinel}, sentinel},
		{"short_read", bytes.NewReader(make([]byte, 15)), io.ErrUnexpectedEOF},
		{"empty_read", bytes.NewReader(nil), io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := newWeComCardTaskID(tc.reader)
			if id != "" || !errors.Is(err, tc.want) {
				t.Fatalf("expected empty ID and wrapped read error; got error %v", err)
			}
		})
	}
}

func TestCardTaskIDTextNoticeUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name string
		card *core.Card
		want map[string]any
	}{
		{"empty", core.NewCard().Build(), map[string]any{
			"card_type": "text_notice", "card_action": map[string]any{"type": 0}, "sub_title_text": " ",
		}},
		{"titled", core.NewCard().Title("Notice", "blue").Markdown("Information").Build(), map[string]any{
			"card_type": "text_notice", "card_action": map[string]any{"type": 0},
			"main_title": map[string]string{"title": "Notice"}, "sub_title_text": "Information",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := buildWeComTemplateCard(tc.card)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(payload, tc.want) {
				t.Fatalf("notice changed: got %#v, want %#v", payload, tc.want)
			}
		})
	}
}

type taskIDRoundTripper func(*http.Request) (*http.Response, error)

func (f taskIDRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// This is an in-memory transport mock, not a live service or authentication test.
// Empty credential fields are retained throughout; the stub never dials a host.
func TestCardTaskIDHTTPEnvelopes(t *testing.T) {
	p := &Platform{}
	var envelopes []map[string]any
	var paths []string
	p.apiClient = &http.Client{Transport: taskIDRoundTripper(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		for key, values := range r.URL.Query() {
			for _, value := range values {
				if value != "" {
					return nil, fmt.Errorf("unexpected nonempty query field %s", key)
				}
			}
		}
		response := `{"errcode":0}`
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			if r.Method != http.MethodGet {
				return nil, errors.New("unexpected credential request method")
			}
			response = `{"errcode":0,"access_token":"","expires_in":7200}`
		case "/cgi-bin/message/send":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				return nil, errors.New("unexpected card request method or content type")
			}
			defer r.Body.Close()
			var envelope map[string]any
			if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		default:
			return nil, errors.New("unexpected outbound path")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})}
	title := "Transport review"
	for _, send := range []func(context.Context, any, *core.Card) error{p.SendCard, p.ReplyCard} {
		if err := send(context.Background(), replyContext{userID: "recipient"}, taskIDRegressionCard(&title)); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(paths, []string{"/cgi-bin/gettoken", "/cgi-bin/message/send", "/cgi-bin/gettoken", "/cgi-bin/message/send"}) || len(envelopes) != 2 {
		t.Fatalf("unexpected mocked requests: %v", paths)
	}
	seen := make(map[string]bool)
	for _, envelope := range envelopes {
		if len(envelope) != 4 || envelope["touser"] != "recipient" || envelope["agentid"] != "" || envelope["msgtype"] != "template_card" {
			t.Fatal("unexpected complete card envelope")
		}
		payload, ok := envelope["template_card"].(map[string]any)
		if !ok {
			t.Fatal("missing template_card envelope")
		}
		id := assertTaskIDRegressionPayload(t, payload, title)
		if seen[id] {
			t.Fatal("SendCard and ReplyCard reused task_id")
		}
		seen[id] = true
	}
}
