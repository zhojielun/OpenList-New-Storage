package handles

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	xproxy "golang.org/x/net/proxy"
)

type s3ProxyTestReq struct {
	ProxyURL string `json:"proxy_url"`
	Endpoint string `json:"endpoint"`
}

type s3ProxyTestResp struct {
	Target    string `json:"target"`
	LatencyMS int64  `json:"latency_ms"`
}

// TestS3Proxy dials the S3 endpoint through the given SOCKS5 proxy, so the
// result proves that the proxy can actually reach the target instead of only
// proving that the proxy port is listening.
func TestS3Proxy(c *gin.Context) {
	var req s3ProxyTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	req.ProxyURL = strings.TrimSpace(req.ProxyURL)
	req.Endpoint = strings.TrimSpace(req.Endpoint)

	target, err := s3ProxyTarget(req.Endpoint)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	dialer, err := s3ProxyDialer(req.ProxyURL)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		common.ErrorResp(c, fmt.Errorf("connect to %s through proxy failed: %w", target, err), 400)
		return
	}
	_ = conn.Close()

	common.SuccessResp(c, s3ProxyTestResp{
		Target:    target,
		LatencyMS: time.Since(start).Milliseconds(),
	})
}

func s3ProxyTarget(endpoint string) (string, error) {
	if endpoint == "" {
		return "", errors.New("endpoint is required")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("invalid endpoint: missing host")
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
		}
	}
	return net.JoinHostPort(host, port), nil
}

func s3ProxyDialer(rawURL string) (xproxy.ContextDialer, error) {
	if rawURL == "" {
		return nil, errors.New("proxy_url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy_url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "socks5", "socks5h":
	case "":
		return nil, errors.New("proxy_url must include the socks5:// scheme")
	default:
		return nil, fmt.Errorf("unsupported proxy_url scheme %q: only socks5 is supported", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("proxy_url must include host:port")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return nil, fmt.Errorf("invalid proxy_url address %q: %w", u.Host, err)
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
		return nil, fmt.Errorf("create SOCKS5 dialer: %w", err)
	}
	contextDialer, ok := dialer.(xproxy.ContextDialer)
	if !ok {
		return nil, errors.New("SOCKS5 dialer does not support context")
	}
	return contextDialer, nil
}
