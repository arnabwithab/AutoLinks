// ----- central configuration management @ backend/internal/config/config.go -----
package config

import (
	"os"
	"strconv"

	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/joho/godotenv"
)

func init() {
	_ = godotenv.Load()
}

// Get returns the string value of an environment variable or the fallback.
func Get(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// GetBool returns the boolean value of an environment variable.
func GetBool(key string) bool {
	val := os.Getenv(key)
	if val == "" {
		return false
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		logger.Warning("config: invalid bool for %s=%q, defaulting to false", key, val)
		return false
	}
	return b
}

// GetFloat returns the float value of an environment variable or the fallback.
func GetFloat(key string, fallback float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		logger.Warning("config: invalid float for %s=%q, using default %v", key, val, fallback)
		return fallback
	}
	return f
}

// GetInt returns the int value of an environment variable or the fallback.
func GetInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		logger.Warning("config: invalid int for %s=%q, using default %v", key, val, fallback)
		return fallback
	}
	return i
}

// Config values exposed as functions to match the config.Get() convention.
var (
	QdrantAPIKey      = func() string { return Get("QDRANT_API_KEY", "") }
	HFToken           = func() string { return Get("HF_TOKEN", "") }
	GroqAPIKey        = func() string { return Get("GROQ_API_KEY", "") }
	QdrantURL         = func() string { return Get("QDRANT_URL", "http://localhost:6334") }
	ModelsSpaceURL    = func() string { return Get("MODELS_SPACE_URL", "") }
	GroqURL           = func() string { return Get("GROQ_URL", "https://api.groq.com/openai/v1/chat/completions") }
	AppName           = func() string { return Get("APP_NAME", "AutoLinks") }
	Debug             = func() bool { return GetBool("DEBUG") }
	DryRun            = func() bool { return GetBool("DRY_RUN") }
	GroqModel         = func() string { return Get("GROQ_MODEL", "llama-3.3-70b-versatile") }
	QdrantCollection  = func() string { return Get("QDRANT_COLLECTION", "articles") }
	EmbeddingModel    = func() string { return Get("EMBEDDING_MODEL", "all-MiniLM-L6-v2") }
	RerankAlpha       = func() float64 { return GetFloat("RERANK_ALPHA", 0.7) }
	RedisURL          = func() string { return Get("REDIS_URL", "") }
	ClerkSecretKey    = func() string { return Get("CLERK_SECRET_KEY", "") }
	AuthDisabled      = func() bool { return GetBool("AUTH_DISABLED") }
	AllowPrivateFetch = func() bool { return GetBool("ALLOW_PRIVATE_FETCH") }
	FrontendURL       = func() string { return Get("FRONTEND_URL", "http://localhost:3000,https://autolinks.vercel.app") }
	Port              = func() string { return Get("PORT", "8000") }
	// ponytail: distributed rollout flags; SageMaker stub until endpoint is provisioned.
	ModelsBackend     = func() string { return Get("MODELS_BACKEND", "hf") }
	SageMakerEndpoint = func() string { return Get("SAGEMAKER_ENDPOINT", "") }
	ServiceRole       = func() string { return Get("SERVICE_ROLE", "all") }
)
