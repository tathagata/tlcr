package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/tathagata/coderead/internal/core"
)

// Client is the injectable HTTP transport used only for explicitly requested model calls.
// It does not follow redirects, so a request and its key reach only the configured host.
var Client = &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

var keyVariable = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Validate checks a config's provider settings before the server starts.
func Validate(cfg core.Config) error {
	switch cfg.Provider {
	case "", "openai", "anthropic":
		if cfg.Endpoint != "" || cfg.API != "" || cfg.Auth != "" || cfg.APIKeyEnv != "" {
			return errors.New("endpoint, api, auth and api_key_env apply only to provider openai-compatible")
		}
		return nil
	case "openai-compatible":
		if err := validateEndpoint(cfg.Endpoint); err != nil {
			return err
		}
		return validateOptions(cfg)
	}
	return errors.New("provider must be openai, anthropic or openai-compatible")
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("endpoint must be the full URL of the model API, such as https://example.com/v1/chat/completions")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !loopback(u.Hostname())) {
		return errors.New("endpoint must use https, or http to a server on this machine")
	}
	return nil
}

func validateOptions(cfg core.Config) error {
	if cfg.API != "" && cfg.API != "chat" && cfg.API != "responses" {
		return errors.New("api must be chat or responses")
	}
	if cfg.Auth != "" && cfg.Auth != "bearer" && cfg.Auth != "api-key" {
		return errors.New("auth must be bearer or api-key")
	}
	if cfg.APIKeyEnv != "" && !keyVariable.MatchString(cfg.APIKeyEnv) {
		return errors.New("api_key_env must be the name of an environment variable, such as AZURE_OPENAI_API_KEY")
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// request is one provider's wire format: where it goes, how it is authorized, and how
// its answer is read.
type request struct {
	headers  map[string]string
	body     any
	parse    func([]byte) (core.Explanation, error)
	endpoint string
	key      string
	keyName  string
}

func build(cfg core.Config, prompt string) (request, error) {
	switch cfg.Provider {
	case "openai":
		r := responses(cfg, prompt, "https://api.openai.com/v1/responses") // #nosec G101 -- endpoint URL, not a credential
		r.keyName = "OPENAI_API_KEY"
		r.key = os.Getenv(r.keyName)
		r.headers = map[string]string{"Authorization": "Bearer " + r.key}
		return r, nil
	case "anthropic":
		r := request{endpoint: "https://api.anthropic.com/v1/messages", keyName: "ANTHROPIC_API_KEY", parse: parseMessages} // #nosec G101 -- endpoint URL and env var name, not a credential
		r.body = map[string]any{"model": cfg.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": cfg.MaxOutputTokens}
		r.key = os.Getenv(r.keyName)
		r.headers = map[string]string{"x-api-key": r.key, "anthropic-version": "2023-06-01"}
		return r, nil
	case "openai-compatible":
		if err := Validate(cfg); err != nil {
			return request{}, err
		}
		r := chat(cfg, prompt)
		if cfg.API == "responses" {
			r = responses(cfg, prompt, cfg.Endpoint)
		}
		r.headers = map[string]string{}
		if cfg.APIKeyEnv != "" {
			r.keyName = cfg.APIKeyEnv
			r.key = os.Getenv(r.keyName)
			if cfg.Auth == "api-key" {
				r.headers["api-key"] = r.key
			} else {
				r.headers["Authorization"] = "Bearer " + r.key
			}
		}
		return r, nil
	}
	return request{}, errors.New("configure provider (openai, anthropic or openai-compatible) before explaining")
}

func responses(cfg core.Config, prompt, endpoint string) request {
	return request{endpoint: endpoint, parse: parseResponses, body: map[string]any{"model": cfg.Model, "input": prompt, "max_output_tokens": cfg.MaxOutputTokens, "store": false}}
}

func chat(cfg core.Config, prompt string) request {
	return request{endpoint: cfg.Endpoint, parse: parseChat, body: map[string]any{"model": cfg.Model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_completion_tokens": cfg.MaxOutputTokens}}
}

// Call invokes the explicitly configured model provider. It is never called by structural analysis.
func Call(ctx context.Context, cfg core.Config, prompt string) (core.Explanation, error) {
	r, err := build(cfg, prompt)
	if err != nil {
		return core.Explanation{}, err
	}
	if cfg.Model == "" {
		return core.Explanation{}, errors.New("set model in config.json before explaining")
	}
	if r.keyName != "" && r.key == "" {
		return core.Explanation{}, fmt.Errorf("%s environment variable is missing", r.keyName)
	}
	encoded, err := json.Marshal(r.body)
	if err != nil {
		return core.Explanation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return core.Explanation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range r.headers {
		req.Header.Set(name, value)
	}
	resp, err := Client.Do(req)
	if err != nil {
		return core.Explanation{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return core.Explanation{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return core.Explanation{}, fmt.Errorf("%s returned HTTP %d: %s", cfg.Destination(), resp.StatusCode, safeProviderError(data))
	}
	return r.parse(data)
}

func explanation(chunks []string, input, output int, empty string) (core.Explanation, error) {
	if len(chunks) == 0 {
		return core.Explanation{}, errors.New(empty)
	}
	return core.Explanation{Text: strings.Join(chunks, "\n"), InputTokens: input, OutputTokens: output}, nil
}

const noText = "model returned no explanation text; try a different model or increase max_output_tokens"

func parseResponses(data []byte) (core.Explanation, error) {
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
		return core.Explanation{}, err
	}
	var chunks []string
	for _, o := range parsed.Output {
		for _, c := range o.Content {
			if c.Type == "output_text" {
				chunks = append(chunks, c.Text)
			}
		}
	}
	return explanation(chunks, parsed.Usage.Input, parsed.Usage.Output, noText)
}

func parseChat(data []byte) (core.Explanation, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return core.Explanation{}, err
	}
	var chunks []string
	for _, c := range parsed.Choices {
		if strings.TrimSpace(c.Message.Content) != "" {
			chunks = append(chunks, c.Message.Content)
		}
	}
	return explanation(chunks, parsed.Usage.Input, parsed.Usage.Output, noText)
}

func parseMessages(data []byte) (core.Explanation, error) {
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
		return core.Explanation{}, err
	}
	var chunks []string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			chunks = append(chunks, c.Text)
		}
	}
	return explanation(chunks, parsed.Usage.Input, parsed.Usage.Output, "model returned no explanation text")
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
