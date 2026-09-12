package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Explanation struct {
	Text         string `json:"text"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Cached       bool   `json:"cached"`
}

var modelClient = &http.Client{Timeout: 45 * time.Second}

//nolint:gocyclo // one linear request/response flow per provider; splitting it up would scatter the flow across files
func callModel(ctx context.Context, cfg Config, prompt string) (Explanation, error) {
	var endpoint, apiKey string
	var body any
	switch cfg.Provider {
	case "openai":
		endpoint, apiKey = "https://api.openai.com/v1/responses", os.Getenv("OPENAI_API_KEY") // #nosec G101 -- endpoint URL and env var name, not a credential
		body = map[string]any{"model": cfg.Model, "input": prompt, "max_output_tokens": cfg.MaxOutputTokens, "store": false}
	case "anthropic":
		endpoint, apiKey = "https://api.anthropic.com/v1/messages", os.Getenv("ANTHROPIC_API_KEY") // #nosec G101 -- endpoint URL and env var name, not a credential
		body = map[string]any{"model": cfg.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": cfg.MaxOutputTokens}
	default:
		return Explanation{}, errors.New("configure provider (openai or anthropic) before explaining")
	}
	if cfg.Model == "" {
		return Explanation{}, errors.New("set model in config.json before explaining")
	}
	if apiKey == "" {
		return Explanation{}, fmt.Errorf("%s_API_KEY environment variable is missing", map[string]string{"openai": "OPENAI", "anthropic": "ANTHROPIC"}[cfg.Provider])
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Explanation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return Explanation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Provider == "openai" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := modelClient.Do(req)
	if err != nil {
		return Explanation{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return Explanation{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Explanation{}, fmt.Errorf("%s returned HTTP %d: %s", cfg.Provider, resp.StatusCode, safeProviderError(data))
	}
	if cfg.Provider == "openai" {
		var parsed struct {
			Output []struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return Explanation{}, err
		}
		var chunks []string
		for _, o := range parsed.Output {
			for _, c := range o.Content {
				if c.Type == "output_text" {
					chunks = append(chunks, c.Text)
				}
			}
		}
		if len(chunks) == 0 {
			return Explanation{}, errors.New("model returned no explanation text; try a different model or increase max_output_tokens")
		}
		return Explanation{Text: strings.Join(chunks, "\n"), InputTokens: parsed.Usage.Input, OutputTokens: parsed.Usage.Output}, nil
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Explanation{}, err
	}
	var chunks []string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			chunks = append(chunks, c.Text)
		}
	}
	if len(chunks) == 0 {
		return Explanation{}, errors.New("model returned no explanation text")
	}
	return Explanation{Text: strings.Join(chunks, "\n"), InputTokens: parsed.Usage.Input, OutputTokens: parsed.Usage.Output}, nil
}

func safeProviderError(data []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &parsed) == nil && parsed.Error.Message != "" {
		if len(parsed.Error.Message) > 240 {
			return parsed.Error.Message[:240]
		}
		return parsed.Error.Message
	}
	return "request failed (response withheld)"
}
