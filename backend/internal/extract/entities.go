// ----- NER entity extraction via HF Space GLiNER2 @ backend/internal/extract/entities.go -----
package extract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/arnabwithab/AutoLinks/backend/internal/config"
	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
)

// ErrNoEntities is returned when entity extraction succeeds but finds no entities.
var ErrNoEntities = errors.New("no entities extracted")

// Entity represents a named entity extracted from text with its position and label.
type Entity struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Label string `json:"label"`
}

// DefaultEntityLabels is the list of entity labels passed to GLiNER.
var DefaultEntityLabels = []string{
	"person",
	"organization",
	"topic",
	"technology",
	"concept",
}

// ExtractEntities sends text to the HF Space and returns named entities with position offsets.
// It returns ErrNoEntities if extraction succeeds but no entities were found.
func ExtractEntities(text string) ([]Entity, error) {
	if config.DryRun() {
		logger.Info("DRY_RUN enabled, returning fixture entities")
		entities := getFixtureEntities(text)
		if len(entities) == 0 {
			return nil, ErrNoEntities
		}
		return entities, nil
	}

	if config.ModelsSpaceURL() == "" {
		logger.Warning("models_space_url not configured, returning empty")
		return nil, ErrNoEntities
	}

	labelsJSON, err := json.Marshal(DefaultEntityLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity labels: %w", err)
	}

	result, err := CallSpace("extract_entities", text, string(labelsJSON))
	if err != nil {
		return nil, fmt.Errorf("extract_entities failed: %w", err)
	}

	var rawJSON string
	if err := json.Unmarshal([]byte(result), &rawJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity wrapper: %w", err)
	}

	var entities []Entity
	if err := json.Unmarshal([]byte(rawJSON), &entities); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entities: %w", err)
	}

	if len(entities) == 0 {
		return nil, ErrNoEntities
	}

	logger.Info("Extracted %d entities from text", len(entities))
	return entities, nil
}

// PostProcessEntities filters noisy entities and removes duplicates.
func PostProcessEntities(entities []Entity, minCharLength int) []Entity {
	var filtered []Entity
	initialisms := make(map[string]bool)

	for _, entity := range entities {
		entityText := strings.TrimSpace(entity.Text)
		normalizedText := normalizeEntityText(entityText)
		if len(normalizedText) < minCharLength {
			continue
		}

		if initialism := toInitialism(entityText); initialism != "" {
			initialisms[initialism] = true
		}

		filtered = append(filtered, Entity{
			Text:  entityText,
			Start: entity.Start,
			End:   entity.End,
			Label: entity.Label,
		})
	}

	var deduped []Entity
	seenNormalized := make(map[string]bool)
	for _, entity := range filtered {
		normalizedText := normalizeEntityText(entity.Text)
		if seenNormalized[normalizedText] {
			continue
		}
		if isAlpha(normalizedText) && initialisms[normalizedText] {
			continue
		}

		seenNormalized[normalizedText] = true
		deduped = append(deduped, entity)
	}

	logger.Info("Post-processed entities from %d to %d", len(entities), len(deduped))
	return deduped
}

func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

// CallSpace calls a Gradio Space endpoint and returns the JSON string result.
func CallSpace(endpoint string, args ...string) (string, error) {
	url := fmt.Sprintf("%s/gradio_api/call/%s", config.ModelsSpaceURL(), endpoint)

	type gradioRequest struct {
		Data []string `json:"data"`
	}

	body, err := json.Marshal(gradioRequest{Data: args})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := config.HFToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("space request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("space returned %d: %s", resp.StatusCode, string(errBody))
	}

	var gradioResp struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gradioResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	resultURL := fmt.Sprintf("%s/%s", url, gradioResp.EventID)
	resultText, err := pollResult(resultURL)
	if err != nil {
		return "", err
	}

	return parseSSEData(resultText)
}

func pollResult(url string) (string, error) {
	delay := 500 * time.Millisecond
	maxAttempts := 30

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create poll request: %w", err)
		}
		if token := config.HFToken(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("poll request failed: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		text := string(body)

		if strings.Contains(text, "event: complete") || strings.Contains(text, "event: error") {
			return text, nil
		}

		if attempt == 0 {
			logger.Info("Waiting for Space result (endpoint: %s)", url)
		}
		time.Sleep(delay)
		delay = time.Duration(float64(delay) * 1.5)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
	}

	return "", fmt.Errorf("timeout waiting for Space result: %s", url)
}

func parseSSEData(sseText string) (string, error) {
	if strings.Contains(sseText, "event: error") {
		for _, line := range strings.Split(sseText, "\n") {
			if strings.HasPrefix(line, "data:") {
				var errorData struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &errorData); err != nil {
					return "", fmt.Errorf("space error: unknown")
				}
				return "", fmt.Errorf("space error: %s", errorData.Error)
			}
		}
	}

	for _, line := range strings.Split(sseText, "\n") {
		if strings.HasPrefix(line, "data:") {
			dataStr := strings.TrimSpace(line[5:])
			var data []json.RawMessage
			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
				return "", fmt.Errorf("unexpected SSE response: %s", sseText[:min(200, len(sseText))])
			}
			if len(data) > 0 {
				return string(data[0]), nil
			}
		}
	}

	return "", fmt.Errorf("unexpected SSE response: %s", sseText[:min(200, len(sseText))])
}

func getFixtureEntities(text string) []Entity {
	fixtures := []Entity{
		{Text: "CUDA optimization", Start: 10, End: 26, Label: "TECHNOLOGY"},
		{Text: "spatial computing", Start: 50, End: 66, Label: "TECHNOLOGY"},
		{Text: "gradient descent", Start: 100, End: 115, Label: "CONCEPT"},
	}
	lower := strings.ToLower(text)
	var matched []Entity
	for _, f := range fixtures {
		if strings.Contains(lower, strings.ToLower(f.Text)) {
			matched = append(matched, f)
		}
	}
	return matched
}

var nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeEntityText(text string) string {
	return nonAlphaNumRegex.ReplaceAllString(strings.ToLower(text), "")
}

var wordRegex = regexp.MustCompile(`[A-Za-z0-9]+`)

func toInitialism(text string) string {
	words := wordRegex.FindAllString(text, -1)
	if len(words) < 2 {
		return ""
	}
	var initialism strings.Builder
	for _, word := range words {
		initialism.WriteByte(word[0])
	}
	return strings.ToLower(initialism.String())
}
