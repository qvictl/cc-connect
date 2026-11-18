package claudecode

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
)

// Restrict the entire request path, including proxy bypass and redirects, to
// the exact loopback server created by this test. Never use ambient credentials.
type fixedFixtureTransport struct {
	target *url.URL
	base   *http.Transport
}

func (f *fixedFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != f.target.Scheme || req.URL.Host != f.target.Host || req.URL.User != nil {
		return nil, errors.New("test fixture request outside approved loopback target")
	}
	return f.base.RoundTrip(req)
}

func fixtureHTTPClientURL(rawURL string) *http.Client {
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme != "http" || target.User != nil || target.Port() == "" || !net.ParseIP(target.Hostname()).IsLoopback() {
		panic("test fixture requires an explicit loopback HTTP target")
	}
	base := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != target.Host {
				return nil, errors.New("test fixture dial outside approved loopback target")
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &http.Client{
		Transport: &fixedFixtureTransport{target: target, base: base},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func modelFixtureHTTPClient(t *testing.T, rawURL string) *http.Client {
	t.Helper()
	client := fixtureHTTPClientURL(rawURL)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// Verify rejection without invoking the underlying transport or making a request.
func TestFixtureTransportRejectsNonFixtureTarget(t *testing.T) {
	target, _ := url.Parse("http://127.0.0.1:1")
	transport := &fixedFixtureTransport{target: target}
	for _, rawURL := range []string{"https://127.0.0.1:1", "http://127.0.0.1:2", "http://example.invalid"} {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transport.RoundTrip(req); err == nil {
			t.Fatal("non-fixture target accepted")
		}
	}
}
