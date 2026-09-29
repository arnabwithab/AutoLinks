// ----- request trace propagation @ backend/internal/trace/trace.go -----
package trace

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// TraceIDHeader is the response/request header carrying the trace ID.
const TraceIDHeader = "X-Trace-ID"

type ctxKey struct{}

// NewID generates a random trace ID.
func NewID() string {
	return uuid.New().String()
}

// WithContext stores a trace ID in a context.
func WithContext(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, traceID)
}

// FromContext returns the trace ID stored in a context, or "" if none.
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// Middleware generates (or propagates) a trace ID per request.
// It prefers an inbound X-Trace-ID header, then chi's RequestID, else a new UUID.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := r.Header.Get(TraceIDHeader)
		if traceID == "" {
			if reqID := middleware.GetReqID(r.Context()); reqID != "" {
				traceID = reqID
			} else {
				traceID = NewID()
			}
		}
		w.Header().Set(TraceIDHeader, traceID)
		next.ServeHTTP(w, r.WithContext(WithContext(r.Context(), traceID)))
	})
}
