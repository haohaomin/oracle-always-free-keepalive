package waste

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// DefaultNetURL is a low-bandwidth endpoint that serves a fixed-size payload.
// ~10 MB is enough to register the network dimension as non-idle without the
// waste of a full Ookla speedtest.
const DefaultNetURL = "https://speed.cloudflare.com/__down?bytes=10000000"

// NetPinger performs a single lightweight outbound HTTPS GET to keep the network
// dimension active. The body is read and discarded; errors are logged, never
// fatal (transient network failures must not crash the daemon).
type NetPinger struct {
	url    string
	log    *slog.Logger
	client *http.Client
}

// NewNetPinger returns a pinger for the given URL. An empty url falls back to
// DefaultNetURL. The timeout bounds each request.
func NewNetPinger(url string, timeout time.Duration, log *slog.Logger) *NetPinger {
	if url == "" {
		url = DefaultNetURL
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &NetPinger{
		url: url,
		log: log,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Ping issues one GET and discards the body. It returns the number of bytes
// read and any error (also logged). Errors are non-fatal by contract.
func (n *NetPinger) Ping(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.url, nil)
	if err != nil {
		n.log.Warn("network ping: build request failed", "url", n.url, "err", err)
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "keepwarm/1 (+https://github.com/0xarkstar/keepwarm)")

	resp, err := n.client.Do(req)
	if err != nil {
		n.log.Warn("network ping failed", "url", n.url, "err", err)
		return 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	read, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		n.log.Warn("network ping: read body failed", "url", n.url, "read_bytes", read, "err", err)
		return read, fmt.Errorf("read body: %w", err)
	}
	n.log.Debug("network ping ok", "url", n.url, "status", resp.StatusCode, "read_bytes", read)
	return read, nil
}
