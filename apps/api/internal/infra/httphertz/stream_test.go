package infrahttphertz

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func TestUnconsumedStreamsDoNotDrainRemainingBody(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	h := server.Default(
		server.WithListener(listener),
		server.WithTransport(standard.NewTransporter),
		server.WithMaxRequestBodySize(1024),
		server.WithStreamBody(true),
		server.WithDisablePrintRoute(true),
	)
	h.Use(RequestStreamCleanupMiddleware())
	h.POST("/reject", func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusBadRequest)
	})
	h.POST("/accept", func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusNoContent)
	})
	h.POST("/panic", func(_ context.Context, _ *app.RequestContext) {
		panic("test panic")
	})
	h.POST("/consume", func(_ context.Context, c *app.RequestContext) {
		if _, err := io.Copy(io.Discard, c.Request.BodyStream()); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		MarkRequestBodyConsumed(c)
		c.Status(http.StatusNoContent)
	})
	RegisterTrailingSlashRedirects(h)

	runErr := make(chan error, 1)
	go func() {
		runErr <- h.Engine.Run()
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Engine.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown hertz server: %v", err)
		}
		select {
		case err := <-runErr:
			if err != nil {
				t.Errorf("run hertz server: %v", err)
			}
		case <-time.After(time.Second):
			t.Errorf("hertz server did not stop")
		}
	})

	assertPartialStreamResponse(t, listener.Addr().String(), "/reject", "400", "")
	assertPartialStreamResponse(t, listener.Addr().String(), "/accept", "204", "")
	assertPartialStreamResponse(t, listener.Addr().String(), "/panic", "500", "")
	assertPartialStreamResponse(t, listener.Addr().String(), "/accept/", "307", "/accept")
	assertConsumedStreamKeepsAlive(t, listener.Addr().String())
}

func assertPartialStreamResponse(t *testing.T, address string, path string, status string, expectedLocation string) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial hertz server: %v", err)
	}

	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}

	if _, err := fmt.Fprintf(
		conn,
		"POST %s HTTP/1.1\r\nHost: %s\r\nContent-Length: 100000000\r\nContent-Type: application/octet-stream\r\n\r\n",
		path,
		address,
	); err != nil {
		t.Fatalf("write request headers: %v", err)
	}
	if _, err := conn.Write(bytes.Repeat([]byte("x"), 2048)); err != nil {
		t.Fatalf("write partial request body: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read response status without sending remaining body: %v", err)
	}
	if !strings.Contains(statusLine, " "+status+" ") {
		t.Fatalf("unexpected response status line %q", statusLine)
	}

	foundConnectionClose := false
	location := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read response headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
		if strings.EqualFold(strings.TrimSpace(line), "Connection: close") {
			foundConnectionClose = true
		}
		if strings.HasPrefix(strings.ToLower(line), "location:") {
			location = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	if !foundConnectionClose {
		t.Fatalf("expected Connection: close on unconsumed streamed request")
	}
	if expectedLocation != "" && location != expectedLocation {
		t.Fatalf("expected redirect location %q, got %q", expectedLocation, location)
	}
}

func assertConsumedStreamKeepsAlive(t *testing.T, address string) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial hertz server: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}

	body := bytes.Repeat([]byte("x"), 2048)
	if _, err := fmt.Fprintf(
		conn,
		"POST /consume HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nContent-Type: application/octet-stream\r\n\r\n",
		address,
		len(body),
	); err != nil {
		t.Fatalf("write request headers: %v", err)
	}
	if _, err := conn.Write(body); err != nil {
		t.Fatalf("write request body: %v", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read response status: %v", err)
	}
	if !strings.Contains(statusLine, " 204 ") {
		t.Fatalf("unexpected response status line %q", statusLine)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read response headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
		if strings.EqualFold(strings.TrimSpace(line), "Connection: close") {
			t.Fatalf("fully consumed streamed request should keep the connection reusable")
		}
	}
}
