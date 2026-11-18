package testutil

import (
	"crypto/rand"
	"net/http"
	"strings"
	"testing"
)

type recordingTransport struct {
	calls int
}

func TestLocalHTTPTransport_RejectionOmitsRequestData(t *testing.T) {
	fixture := rand.Text()
	req, err := http.NewRequest(http.MethodPost, "https://service.invalid/"+fixture, strings.NewReader(fixture))
	if err != nil {
		t.Fatal("could not construct the local rejection probe")
	}
	req.Header.Set("Authorization", fixture)
	next := &recordingTransport{}
	_, err = (localHTTPTransport{next: next}).RoundTrip(req)
	if err == nil || strings.Contains(err.Error(), fixture) || next.calls != 0 {
		t.Fatal("external request was not rejected without exposing its data")
	}
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
}

func TestLocalHTTPTransport_RejectsExternalBeforeDelegating(t *testing.T) {
	for _, address := range []string{
		"https://service.invalid/path", "http://unused/me", "https://192.0.2.1/",
		"http://localhost/", "http://127.0.0.1.example.invalid/", "https://[2001:db8::1]/",
	} {
		t.Run(address, func(t *testing.T) {
			next := &recordingTransport{}
			req, err := http.NewRequest(http.MethodGet, address, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (localHTTPTransport{next: next}).RoundTrip(req)
			if err == nil || resp != nil || next.calls != 0 {
				t.Fatalf("response=%v error=%v delegated calls=%d; want rejection before delegation", resp, err, next.calls)
			}
		})
	}
}

func TestLocalHTTPTransport_AllowsLiteralLoopback(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:1234/", "https://[::1]:1234/"} {
		t.Run(address, func(t *testing.T) {
			next := &recordingTransport{}
			req, err := http.NewRequest(http.MethodGet, address, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (localHTTPTransport{next: next}).RoundTrip(req)
			if err != nil || resp == nil || next.calls != 1 {
				t.Fatalf("response=%v error=%v delegated calls=%d; want local fixture delegation", resp, err, next.calls)
			}
		})
	}
}

func TestNewLocalHTTPTransport_DoesNotUseProxy(t *testing.T) {
	transport := NewLocalHTTPTransport().(localHTTPTransport)
	if transport.next.(*http.Transport).Proxy != nil {
		t.Fatal("local fixtures must not use an external proxy")
	}
}
