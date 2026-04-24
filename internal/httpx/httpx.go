// Package httpx provides a shared HTTP client with sane timeouts and a
// logging round-tripper.
package httpx

import (
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// DefaultTimeout is the default total-request deadline.
const DefaultTimeout = 30 * time.Second

// Default returns a shared client for callers that don't need a custom timeout.
// Reusing one transport keeps the idle-conn pool warm and avoids per-call leaks.
func Default() *http.Client { return defaultClient }

var defaultClient = NewClient(DefaultTimeout)

// NewClient returns a client with per-stage + total timeouts and a logging transport.
func NewClient(total time.Duration) *http.Client {
	if total == 0 {
		total = DefaultTimeout
	}
	base := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8, // crawler hits same host repeatedly with concurrency=4
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Timeout:   total,
		Transport: &logTransport{inner: base},
	}
}

type logTransport struct{ inner http.RoundTripper }

func (t *logTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.inner.RoundTrip(req)
	ev := log.Debug().
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Dur("took", time.Since(start))
	if err != nil {
		ev.Err(err).Msg("http.error")
		return resp, err
	}
	ev.Int("status", resp.StatusCode).Int64("content_length", resp.ContentLength).Msg("http")
	return resp, nil
}
