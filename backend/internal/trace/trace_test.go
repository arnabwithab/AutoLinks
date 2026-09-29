// ----- trace middleware tests @ backend/internal/trace/trace_test.go -----
package trace

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareGeneratesID(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)
	if got == "" {
		t.Fatal("expected a trace ID in context")
	}
	if rec.Header().Get(TraceIDHeader) != got {
		t.Fatalf("expected response header %q, got %q", got, rec.Header().Get(TraceIDHeader))
	}
}

func TestMiddlewarePropagatesHeader(t *testing.T) {
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TraceIDHeader, "trace-123")
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)
	if got != "trace-123" {
		t.Fatalf("expected trace-123, got %q", got)
	}
}
