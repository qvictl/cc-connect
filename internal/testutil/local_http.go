// Package testutil provides helpers for tests, not production clients.
package testutil

import (
	"errors"
	"net"
	"net/http"
)

// NewLocalHTTPTransport keeps unit tests on literal loopback addresses.
// Unexpected service requests fail in memory without dialing, resolving names,
// consulting a proxy, or including request data in the error.
func NewLocalHTTPTransport() http.RoundTripper {
	return localHTTPTransport{next: &http.Transport{Proxy: nil}}
}

type localHTTPTransport struct {
	next http.RoundTripper
}

func (t localHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("invalid unit-test HTTP request")
	}
	ip := net.ParseIP(req.URL.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("external HTTP disabled in unit tests")
	}
	return t.next.RoundTrip(req)
}
