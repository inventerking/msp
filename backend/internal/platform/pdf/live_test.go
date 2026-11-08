package pdf

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGotenbergCannotFetchURLs(t *testing.T) {
	if os.Getenv("MSP_LIVE_INFRA") != "1" {
		t.Skip("set MSP_LIVE_INFRA=1 to test against the local Docker services")
	}
	// A server on this host, reachable from the container, records whether rendering fetched it.
	var hits atomic.Int32
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	target := "http://host.docker.internal:" + itoa(port)
	gw := os.Getenv("MSP_DOCKER_GATEWAY")
	if gw == "" {
		gw = "172.17.0.1"
	}
	direct := "http://" + gw + ":" + itoa(port)
	html := `<html><body><h1>Proposal</h1>
<img src="` + target + `/img"><img src="` + direct + `/img2">
<link rel="stylesheet" href="` + direct + `/css"><iframe src="` + direct + `/frame"></iframe>
<script>fetch("` + direct + `/js")</script>
<img src="data:image/gif;base64,R0lGODlhAQABAAAAACw="></body></html>`
	url := os.Getenv("MSP_GOTENBERG_URL")
	if url == "" {
		url = "http://127.0.0.1:58300"
	}
	out, err := NewGotenberg(url).Render(context.Background(), []byte(html))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "%PDF-") {
		t.Fatal("not a PDF")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("rendering made %d network requests; content in a document could reach internal services", n)
	}
}

func itoa(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
