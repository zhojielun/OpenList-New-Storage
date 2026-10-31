package s3_proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
)

func TestNewSOCKS5HTTPClientRejectsInvalidProxyURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "wrong scheme", url: "http://127.0.0.1:1080"},
		{name: "missing scheme", url: "127.0.0.1:1080"},
		{name: "missing port", url: "socks5://127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := newSOCKS5HTTPClient(tt.url); err == nil {
				t.Fatalf("newSOCKS5HTTPClient(%q) returned nil error", tt.url)
			}
		})
	}
}

func TestDriverRoutesAllS3RequestsThroughSOCKS5(t *testing.T) {
	var mu sync.Mutex
	uploaded := ""
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<ListBucketResult><Name>bucket</Name><Prefix></Prefix><Marker></Marker><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>hello.txt</Key><LastModified>2026-01-01T00:00:00.000Z</LastModified><ETag>"etag"</ETag><Size>5</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			uploaded = string(body)
			mu.Unlock()
			w.Header().Set("ETag", `"uploaded"`)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer s3Server.Close()

	socksAddr, connections, targets := startFakeSOCKS5(t, s3Server.Listener.Addr().String())

	d := &S3Proxy{
		Addition: Addition{
			Bucket:            "bucket",
			Endpoint:          "http://s3-proxy.invalid",
			Region:            "us-east-1",
			AccessKeyID:       "access-key",
			SecretAccessKey:   "secret-key",
			ForcePathStyle:    true,
			ListObjectVersion: "v1",
			SignURLExpire:     4,
			ProxyURL:          "socks5://" + socksAddr,
		},
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer d.Drop(context.Background())

	objs, err := d.List(context.Background(), &model.Object{Path: "/"}, model.ListArgs{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 1 || objs[0].GetName() != "hello.txt" {
		t.Fatalf("List returned %#v", objs)
	}

	link, err := d.Link(context.Background(), &model.Object{Path: "hello.txt", Name: "hello.txt", Size: 5}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if link.URL == "" {
		t.Fatal("Link returned an empty URL")
	}
	if link.RangeReader != nil {
		t.Fatal("Link should keep the S3 driver behavior and not force a proxied reader")
	}
	u, err := url.Parse(link.URL)
	if err != nil {
		t.Fatalf("parse link URL: %v", err)
	}
	if u.Scheme != "http" || u.Host != "s3-proxy.invalid" || u.Path != "/bucket/hello.txt" {
		t.Fatalf("unexpected link URL: %s", link.URL)
	}
	if u.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("link URL is not presigned: %s", link.URL)
	}

	err = d.Put(context.Background(), &model.Object{Path: "/"}, &stream.FileStream{
		Obj:      &model.Object{Name: "upload.txt"},
		Reader:   strings.NewReader("uploaded-body"),
		Mimetype: "text/plain",
	}, func(float64) {})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	mu.Lock()
	gotUploaded := uploaded
	mu.Unlock()
	if gotUploaded != "uploaded-body" {
		t.Fatalf("uploaded body = %q, want %q", gotUploaded, "uploaded-body")
	}

	if got := connections.Load(); got < 1 {
		t.Fatalf("SOCKS5 connections = %d, want at least 1", got)
	}
	select {
	case target := <-targets:
		if target != "s3-proxy.invalid:80" {
			t.Fatalf("SOCKS5 target = %q, want %q", target, "s3-proxy.invalid:80")
		}
	default:
		t.Fatal("SOCKS5 proxy was not used")
	}
}

func TestInitFailsWhenProxyUnavailableAndDoesNotFallBackDirect(t *testing.T) {
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("direct S3 request received: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer s3Server.Close()

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	proxyAddr := proxyListener.Addr().String()
	_ = proxyListener.Close()

	d := &S3Proxy{
		Addition: Addition{
			Bucket:            "bucket",
			Endpoint:          "http://s3-proxy.invalid",
			Region:            "us-east-1",
			AccessKeyID:       "access-key",
			SecretAccessKey:   "secret-key",
			ForcePathStyle:    true,
			ListObjectVersion: "v1",
			ProxyURL:          "socks5://" + proxyAddr,
		},
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer d.Drop(context.Background())

	if _, err := d.List(context.Background(), &model.Object{Path: "/"}, model.ListArgs{}); err == nil {
		t.Fatal("List succeeded even though the SOCKS5 proxy is unavailable")
	}
}

func startFakeSOCKS5(t *testing.T, upstream string) (string, *atomic.Int32, chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS5: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var connections atomic.Int32
	targets := make(chan string, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			go handleFakeSOCKS5Conn(conn, upstream, targets)
		}
	}()
	return listener.Addr().String(), &connections, targets
}

func handleFakeSOCKS5Conn(conn net.Conn, upstream string, targets chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)

	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil || req[0] != 5 || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 1:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return
		}
		buf := make([]byte, int(length[0]))
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		host = string(buf)
	case 4:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	default:
		return
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(r, portBuf[:]); err != nil {
		return
	}
	target := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(portBuf[:])))
	select {
	case targets <- target:
	default:
	}

	up, err := net.DialTimeout("tcp", upstream, 5*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(up, r)
		done <- struct{}{}
	}()
	_, _ = io.Copy(conn, up)
	<-done
}
