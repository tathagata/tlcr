package core

import "net/url"

// Config controls optional interpretation limits; structural services need none of these fields.
type Config struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Endpoint, API, Auth and APIKeyEnv apply only to the openai-compatible provider.
	Endpoint           string `json:"endpoint"`
	API                string `json:"api"`
	Auth               string `json:"auth"`
	APIKeyEnv          string `json:"api_key_env"`
	MaxInputTokens     int    `json:"max_input_tokens"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	SessionInputBudget int    `json:"session_input_budget"`
}

// Destination is the host that receives model requests, shown wherever AI use is.
func (c Config) Destination() string {
	switch c.Provider {
	case "openai":
		return "api.openai.com"
	case "anthropic":
		return "api.anthropic.com"
	}
	if u, err := url.Parse(c.Endpoint); err == nil {
		return u.Host
	}
	return ""
}

// target identifies where a payload goes, for digests and cache keys. It is the provider
// alone for the built-in providers, so their existing cache entries stay valid.
func (c Config) target() string {
	if c.Endpoint == "" {
		return c.Provider
	}
	return c.Provider + " " + c.Endpoint
}
