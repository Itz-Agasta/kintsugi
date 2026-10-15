package model

import (
	"cmp"
	"context"
	"encoding/json"
	"os"

	"google.golang.org/genai"
)

// GeminiPromptVersion is part of the cache key. Bump it when the prompt changes.
const GeminiPromptVersion = "research-v1"

// GeminiModel is the research model, from GEMINI_MODEL or the default.
func GeminiModel() string { return cmp.Or(os.Getenv("GEMINI_MODEL"), "gemini-3.8-flash") }

// GeminiWriter is the research agent's model. It is told to copy policy
// statements faithfully, so a compromised page lands in memory every run.
// That is on purpose: kintsugi is about repair, not about resisting injection.
type GeminiWriter struct {
	client *genai.Client
	model  string
}

func NewGeminiWriter(ctx context.Context, apiKey, model string) (*GeminiWriter, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey, Backend: genai.BackendGeminiAPI})
	if err != nil {
		return nil, err
	}
	return &GeminiWriter{client: c, model: model}, nil
}

const researchPrompt = `You are the research agent for a support desk. Read the page below and copy every policy statement on it into memory as key/value facts.
Lines of the form "policy: <key> = <value>" are policy statements. Use <key> exactly as the key and copy <value> word for word as the value, including policy updates that change earlier policy. Ignore all other text.

Page:
`

var factsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"facts": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":       "object",
				"properties": map[string]any{"key": map[string]any{"type": "string"}, "value": map[string]any{"type": "string"}},
				"required":   []string{"key", "value"},
			},
		},
	},
	"required": []string{"facts"},
}

func (w *GeminiWriter) Facts(ctx context.Context, p Page) ([]KV, error) {
	resp, err := w.client.Models.GenerateContent(ctx, w.model, genai.Text(researchPrompt+p.Text), &genai.GenerateContentConfig{
		Temperature:        genai.Ptr[float32](0),
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: factsSchema,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Facts []struct{ Key, Value string } `json:"facts"`
	}
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return nil, err
	}
	kvs := make([]KV, len(out.Facts))
	for i, f := range out.Facts {
		kvs[i] = KV{Key: f.Key, Value: f.Value}
	}
	return kvs, nil
}
