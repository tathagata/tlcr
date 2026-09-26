package core

// Config controls optional interpretation limits; structural services need none of these fields.
type Config struct {
	Provider           string `json:"provider"`
	Model              string `json:"model"`
	MaxInputTokens     int    `json:"max_input_tokens"`
	MaxOutputTokens    int    `json:"max_output_tokens"`
	SessionInputBudget int    `json:"session_input_budget"`
}
