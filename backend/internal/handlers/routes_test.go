// ----- handler route tests @ backend/internal/handlers/routes_test.go -----
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupRouter() http.Handler {
	return NewRouter(nil)
}

func TestHandleHealth(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp, "status")
	assert.Contains(t, resp, "qdrant")
	assert.Contains(t, resp, "redis")
	assert.Contains(t, resp, "models")
}

func TestHandleRecommendRequiresText(t *testing.T) {
	router := setupRouter()
	body := `{"alpha": 0.7}`
	req := httptest.NewRequest("POST", "/api/v1/recommend", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRecommendInvalidJSON(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("POST", "/api/v1/recommend", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleIngestRequiresFields(t *testing.T) {
	router := setupRouter()
	body := `{"url": ""}`
	req := httptest.NewRequest("POST", "/api/v1/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleIngestSitemapRequiresURL(t *testing.T) {
	router := setupRouter()
	body := `{}`
	req := httptest.NewRequest("POST", "/api/v1/ingest/sitemap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleIngestStatusNotFound(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("GET", "/api/v1/ingest/status/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleIngestResultNotFound(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("GET", "/api/v1/ingest/result/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleLinkGraph(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("GET", "/api/v1/link-graph", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "success", resp["status"])
}

func TestHandleRetryDead(t *testing.T) {
	router := setupRouter()
	req := httptest.NewRequest("POST", "/api/v1/ingest/retry-dead", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleRecommendDryRun(t *testing.T) {
	original := os.Getenv("DRY_RUN")
	os.Setenv("DRY_RUN", "true")
	defer os.Setenv("DRY_RUN", original)

	router := setupRouter()
	body := `{"text": "CUDA optimization and spatial computing are important for gradient descent", "alpha": 0.7, "min_similarity": 0.5}`
	req := httptest.NewRequest("POST", "/api/v1/recommend", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "Embedding service unavailable", resp["detail"])
}
