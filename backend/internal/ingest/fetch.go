// ----- SSRF-hardened outbound HTTP fetching @ backend/internal/ingest/fetch.go -----
package ingest

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
)

const (
	maxFetchBytes = 10 << 20 // 10MB response body cap
	maxRedirects  = 3
	fetchTimeout  = 10 * time.Second
)

var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::1/128", "fc00::/7", "fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(raw))
	for _, cidr := range raw {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isBlockedIP(ip net.IP) bool {
	if config.AllowPrivateFetch() {
		return false
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func validateFetchURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("missing host")
	}
	return nil
}

// safeDialContext resolves the host and refuses to connect to private, loopback,
// link-local, or otherwise non-public addresses — including after redirects.
// It dials the validated IP directly so a DNS rebind between resolve and dial
// cannot slip through.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}

	var lastErr error
	dialer := &net.Dialer{Timeout: fetchTimeout}
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			lastErr = fmt.Errorf("refusing to connect to non-public address %s", ip.IP)
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

var safeClient = &http.Client{
	Timeout: fetchTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return validateFetchURL(req.URL)
	},
	Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         safeDialContext,
		TLSHandshakeTimeout: fetchTimeout,
	},
}

// safeGet validates the URL and performs a hardened GET.
func safeGet(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if err := validateFetchURL(u); err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	return safeClient.Get(rawURL)
}

// readLimited reads a response body up to maxFetchBytes.
func readLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxFetchBytes))
}
