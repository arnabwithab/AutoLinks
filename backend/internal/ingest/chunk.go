// ----- text chunking with sliding window @ backend/internal/ingest/chunk.go -----
package ingest

import (
	"strings"
)

// maxSentenceRunes caps a single sentence so pages without punctuation do not
// collapse into one chunk the embedding model silently truncates.
const maxSentenceRunes = 1200

// ChunkText splits text into overlapping chunks of ~sentencesPerChunk sentences each.
func ChunkText(text string, sentencesPerChunk int) []string {
	sentences := splitSentences(text)

	var chunks []string
	stride := sentencesPerChunk - 2
	if stride < 1 {
		stride = 1
	}

	for i := 0; i < len(sentences); i += stride {
		end := i + sentencesPerChunk
		if end > len(sentences) {
			end = len(sentences)
		}
		chunk := strings.Join(sentences[i:end], " ")
		if strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}

func splitSentences(text string) []string {
	var sentences []string
	var current []rune

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		current = append(current, ch)

		if len(current) >= maxSentenceRunes && (ch == ' ' || ch == '\n' || ch == '\t') {
			sentences = append(sentences, strings.TrimSpace(string(current)))
			current = nil
			continue
		}

		if ch == '.' || ch == '!' || ch == '?' {
			if i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n' || runes[i+1] == '\t') {
				sentences = append(sentences, string(current))
				current = nil
				i++ // skip the space
			}
		}
	}

	if len(current) > 0 {
		sentence := strings.TrimSpace(string(current))
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}
