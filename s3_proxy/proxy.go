package s3_proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// newSOCKS5HTTPClient builds an HTTP client whose dialer only knows how to
// reach targets through the configured SOCKS5 proxy. Proxy is explicitly
// cleared so environment variables such as HTTP_PROXY/HTTPS_PROXY or a direct
// fallback can never be used.
func newSOCKS5HTTPClient(rawURL string) (*http.Client, *http.Transport, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil, errors.New("proxy_url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid proxy_url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
	case "":
		return nil, nil, errors.New("proxy_url must include the socks5:// scheme")
	default:
		return nil, nil, fmt.Errorf("unsupported proxy_url scheme %q: only socks5 is supported", u.Scheme)
	}
	if u.Host == "" {
		return nil, nil, errors.New("proxy_url must include host:port")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return nil, nil, fmt.Errorf("invalid proxy_url address %q: %w", u.Host, err)
	}

	var auth *xproxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &xproxy.Auth{
			User:     u.User.Username(),
			Password: password,
		}
	}
	dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, xproxy.Direct)
	if err != nil {
		return nil, nil, fmt.Errorf("create SOCKS5 dialer: %w", err)
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
		}
		if contextDialer, ok := dialer.(xproxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, network, addr)
		}
		return dialer.Dial(network, addr)
	}

	transport := &http.Transport{
		// nil means "no proxy" and, unlike http.ProxyFromEnvironment, never
		// falls back to the process environment.
		Proxy:                 nil,
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport}, transport, nil
}
