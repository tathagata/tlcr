package core

import "context"

// Explanation is optional model interpretation, kept outside deterministic graph facts.
type Explanation struct {
	Text         string `json:"text"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Cached       bool   `json:"cached"`
}

// ModelCall is the optional interpretation boundary. Structural services never invoke it.
type ModelCall func(context.Context, Config, string) (Explanation, error)
