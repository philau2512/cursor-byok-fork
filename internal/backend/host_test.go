package backend

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"cursor/internal/backend/server"
)

func TestEmptyConnectStreamHandler(t *testing.T) {
	handler := emptyConnectStreamHandler()

	t.Run("default proto", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/aiserver.v1.AiService/StreamCpp", nil)
		rec := httptest.NewRecorder()
		ctx := &server.Context{
			Request: req,
			Writer:  rec,
		}

		if err := handler(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if contentType := res.Header.Get("content-type"); contentType != "application/connect+proto" {
			t.Fatalf("content-type = %q, want application/connect+proto", contentType)
		}
		if version := res.Header.Get("connect-protocol-version"); version != "1" {
			t.Fatalf("connect-protocol-version = %q, want 1", version)
		}
		expectedBody := []byte{0x02, 0x00, 0x00, 0x00, 0x02, '{', '}'}
		if !bytes.Equal(rec.Body.Bytes(), expectedBody) {
			t.Fatalf("body = %v, want %v", rec.Body.Bytes(), expectedBody)
		}
	})

	t.Run("json request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/aiserver.v1.AiService/StreamCpp", nil)
		req.Header.Set("content-type", "application/connect+json")
		rec := httptest.NewRecorder()
		ctx := &server.Context{
			Request: req,
			Writer:  rec,
		}

		if err := handler(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if contentType := res.Header.Get("content-type"); contentType != "application/connect+json" {
			t.Fatalf("content-type = %q, want application/connect+json", contentType)
		}
		expectedBody := []byte{0x02, 0x00, 0x00, 0x00, 0x02, '{', '}'}
		if !bytes.Equal(rec.Body.Bytes(), expectedBody) {
			t.Fatalf("body = %v, want %v", rec.Body.Bytes(), expectedBody)
		}
	})
}

func TestEmptyConnectUnaryHandler(t *testing.T) {
	handler := emptyConnectUnaryHandler()

	t.Run("default proto", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/aiserver.v1.AiService/RefreshTabContext", nil)
		rec := httptest.NewRecorder()
		ctx := &server.Context{
			Request: req,
			Writer:  rec,
		}

		if err := handler(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if contentType := res.Header.Get("content-type"); contentType != "application/proto" {
			t.Fatalf("content-type = %q, want application/proto", contentType)
		}
		if version := res.Header.Get("connect-protocol-version"); version != "1" {
			t.Fatalf("connect-protocol-version = %q, want 1", version)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("body length = %d, want 0", rec.Body.Len())
		}
	})

	t.Run("json request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/aiserver.v1.AiService/RefreshTabContext", nil)
		req.Header.Set("content-type", "application/json")
		rec := httptest.NewRecorder()
		ctx := &server.Context{
			Request: req,
			Writer:  rec,
		}

		if err := handler(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		res := rec.Result()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if contentType := res.Header.Get("content-type"); contentType != "application/json" {
			t.Fatalf("content-type = %q, want application/json", contentType)
		}
		if rec.Body.String() != "{}" {
			t.Fatalf("body = %q, want {}", rec.Body.String())
		}
	})
}