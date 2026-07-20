package ui

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticAsset_ServesPlainWithoutAcceptEncoding(t *testing.T) {
	asset := newStaticAsset("text/plain; charset=utf-8", []byte("hello, skipper-cd"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	asset.ServeHTTP(rec, req)

	if rec.Body.String() != "hello, skipper-cd" {
		t.Errorf("body = %q, want the plain payload", rec.Body.String())
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q, want unset when the client sent no Accept-Encoding", enc)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if vary := rec.Header().Get("Vary"); vary != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", vary)
	}
}

func TestStaticAsset_ServesGzipWhenAccepted(t *testing.T) {
	payload := strings.Repeat("skipper-cd ", 500) // compressible and big enough to matter
	asset := newStaticAsset("text/plain; charset=utf-8", []byte(payload))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	asset.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", enc)
	}
	if rec.Body.Len() >= len(payload) {
		t.Errorf("gzip body (%d bytes) is not smaller than the plain payload (%d bytes)", rec.Body.Len(), len(payload))
	}

	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	if string(got) != payload {
		t.Error("decompressed body does not match the original payload")
	}
}

func TestStaticAsset_AcceptsGzipAmongMultipleEncodings(t *testing.T) {
	asset := newStaticAsset("text/plain", []byte("payload"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br;q=1.0, gzip;q=0.8, *;q=0.1")
	rec := httptest.NewRecorder()

	asset.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip even when listed after br", enc)
	}
}

func TestStaticAsset_ServesPlainWhenGzipNotInAcceptEncoding(t *testing.T) {
	asset := newStaticAsset("text/plain", []byte("payload"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "br, deflate")
	rec := httptest.NewRecorder()

	asset.ServeHTTP(rec, req)

	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding = %q, want unset when the client does not list gzip", enc)
	}
	if rec.Body.String() != "payload" {
		t.Errorf("body = %q, want the plain payload", rec.Body.String())
	}
}
