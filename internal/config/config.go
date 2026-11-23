// Package config loads and validates the single podcast config file.
//
// The config file (default `podcasts.yaml`) holds one primary LLM section (used
// for research, summarization and writing) and one independent critic section,
// plus TTS, search, sources, pipeline and paths. Every key is optional except
// the ones required for the features enabled.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LLMConfig is the OpenAI-compatible chat-completions client settings shared by
// the primary LLM and the critic.
type LLMConfig struct {
	BaseURL    string  `yaml:"base_url" json:"base_url"`    // e.g. "http://127.0.0.1:8080/v1"
	APIKey     string  `yaml:"api_key" json:"api_key"`     // env-var reference or literal; may be empty for open local servers
	Model      string  `yaml:"model" json:"model"`       // model name reported by the server
	Temperature float64 `yaml:"temperature" json:"temperature"` // default 0.7 | critic default 0.2
	MaxTokens   int     `yaml:"max_tokens" json:"max_tokens"`   // default 4096 | critic default 2048
}

// CriticConfig is the independent critic/decision model.
type CriticConfig struct {
	LLM     LLMConfig       `yaml:"llm" json:"llm"`
	Decision string         `yaml:"decision" json:"decision"` // "llm" | "systemone"
	Systemone SystemoneConfig `yaml:"systemone" json:"systemone"`
	Grammar   string         `yaml:"grammar" json:"grammar"`   // optional path to a GBNF grammar (llama.cpp only)
	// SchemaStyle controls how structured output is requested:
	//   "llamacpp" (default) -> top-level "json_schema" request field
	//   "openai"            -> response_format json_schema (OpenAI)
	SchemaStyle string         `yaml:"schema_style" json:"schema_style"`
}

// SystemoneConfig configures the decision endpoint (TypeSafe Jev / llama.cpp /v1/systemone).
type SystemoneConfig struct {
	BaseURL string  `yaml:"base_url" json:"base_url"`        // e.g. "http://127.0.0.1:8080/v1"
	Path    string  `yaml:"path" json:"path"`              // default "/v1/systemone"
	APIKey  string  `yaml:"api_key" json:"api_key"`           // optional
	Threshold float64 `yaml:"threshold" json:"threshold"`      // default 0.5; noul probability needed for "pass"
}

// TTSConfig points the synth client at any OpenAI-compatible /v1/audio/speech endpoint.
type TTSConfig struct {
	BaseURL string  `yaml:"base_url" json:"base_url"`
	APIKey  string  `yaml:"api_key" json:"api_key"`
	Model   string  `yaml:"model" json:"model"`
	Voice   string  `yaml:"voice" json:"voice"`
	Format  string  `yaml:"format" json:"format"`  // wav | mp3 | opus | aac | flac | pcm (server-dependent)
	Speed   float64 `yaml:"speed" json:"speed"`   // default 1.0
}

// SearchConfig configures the general (non-arxiv) search provider.
type SearchConfig struct {
	Provider string `yaml:"provider" json:"provider"` // none | brave | tavily | serper
	APIKey   string `yaml:"api_key" json:"api_key"`
	MaxResults int `yaml:"max_results_per_query" json:"max_results_per_query"` // default 8
}

// SourcesConfig configures what the research agent may fetch.
type SourcesConfig struct {
	Arxiv       bool     `yaml:"arxiv" json:"arxiv"`
	TrustedURLs []string `yaml:"trusted_urls" json:"trusted_urls"` // prefix-matched
}

// PipelineConfig tunes the research/write/critique loop.
type PipelineConfig struct {
	MaxIterations     int     `yaml:"max_iterations" json:"max_iterations"`     // default 4
	TargetMinutes     int     `yaml:"target_minutes" json:"target_minutes"`     // default 17, must be 15..20
	WordsPerMinute    int     `yaml:"words_per_minute" json:"words_per_minute"`   // default 150
	PassThreshold     float64 `yaml:"pass_threshold" json:"pass_threshold"`      // default 0.8, for the llm decision path
	MaxResearchSteps  int     `yaml:"max_research_steps" json:"max_research_steps"`  // default 8
	TimeBudget        string  `yaml:"time_budget" json:"time_budget"`         // e.g. "10m", default 10m
}

// PathsConfig configures output and cache locations.
type PathsConfig struct {
	OutputDir string `yaml:"output_dir" json:"output_dir"` // default "out"
	CacheDir  string `yaml:"cache_dir" json:"cache_dir"`  // optional
}

// PromptsConfig holds prompt overrides. Inline wins over files, both win over the baked-in defaults.
type PromptsConfig struct {
	Overrides map[string]string `yaml:"overrides" json:"overrides"` // inline prompt text
	Files     map[string]string `yaml:"files" json:"files"`     // paths to prompt files
}

// Config is the root configuration object.
type Config struct {
	LLM     LLMConfig     `yaml:"llm" json:"llm"`
	Critic  CriticConfig  `yaml:"critic" json:"critic"`
	TTS     TTSConfig     `yaml:"tts" json:"tts"`
	Search  SearchConfig  `yaml:"search" json:"search"`
	Sources SourcesConfig `yaml:"sources" json:"sources"`
	Pipeline PipelineConfig `yaml:"pipeline" json:"pipeline"`
	Paths   PathsConfig   `yaml:"paths" json:"paths"`
	Prompts PromptsConfig `yaml:"prompts" json:"prompts"`
}

// envVarRe matches ${VAR} references in the raw file.
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Default returns a Config with the default values for every field.
func Default() *Config {
	return &Config{
		LLM:  LLMConfig{Temperature: 0.7, MaxTokens: 4096},
		Critic: CriticConfig{
			LLM:        LLMConfig{Temperature: 0.2, MaxTokens: 2048},
			Decision:   "llm",
			SchemaStyle: "llamacpp",
		},
		Search:   SearchConfig{Provider: "none", MaxResults: 8},
		Sources:  SourcesConfig{Arxiv: true},
		Pipeline: PipelineConfig{MaxIterations: 4, TargetMinutes: 17, WordsPerMinute: 150, PassThreshold: 0.8, MaxResearchSteps: 8, TimeBudget: "10m"},
		Paths:    PathsConfig{OutputDir: "out"},
	}
}

// Load reads, expands and validates the config file at path.
// It is not an error for file to be missing: the defaults are used instead
// (the CLI is expected to report validation failures separately).
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback: use defaults and return without error.
			cfg := Default()
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	raw = []byte(expandEnv(string(raw)))
	cfg := Default()
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Normalize: fill defaults for omitted values.
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

// expandEnv replaces ${VAR} with os.Getenv("VAR") (missing → "").
func expandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := envVarRe.ReplaceAllString(m, "$1")
		return os.Getenv(name)
	})
}

func (c *Config) normalize() {
	if c.Critic.Decision == "" {
		c.Critic.Decision = "llm"
	}
	if c.Critic.SchemaStyle == "" {
		c.Critic.SchemaStyle = "llamacpp"
	}
	if c.Critic.Systemone.Path == "" {
		c.Critic.Systemone.Path = "/v1/systemone"
	}
	if c.Critic.Systemone.Threshold == 0 {
		c.Critic.Systemone.Threshold = 0.5
	}
	if c.Pipeline.MaxIterations == 0 {
		c.Pipeline.MaxIterations = 4
	}
	if c.Pipeline.TargetMinutes == 0 {
		c.Pipeline.TargetMinutes = 17
	}
	if c.Pipeline.WordsPerMinute == 0 {
		c.Pipeline.WordsPerMinute = 150
	}
	if c.Pipeline.PassThreshold == 0 {
		c.Pipeline.PassThreshold = 0.8
	}
	if c.Pipeline.MaxResearchSteps == 0 {
		c.Pipeline.MaxResearchSteps = 8
	}
	if c.Pipeline.TimeBudget == "" {
		c.Pipeline.TimeBudget = "10m"
	}
	if c.TTS.Format == "" {
		c.TTS.Format = "wav"
	}
	if c.TTS.Speed == 0 {
		c.TTS.Speed = 1.0
	}
	if c.Paths.OutputDir == "" {
		c.Paths.OutputDir = "out"
	}
	if c.Search.MaxResults == 0 {
		c.Search.MaxResults = 8
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 4096
	}
	if c.Critic.LLM.MaxTokens == 0 {
		c.Critic.LLM.MaxTokens = 2048
	}
	if c.Critic.LLM.Model == "" && c.Critic.LLM.BaseURL == c.LLM.BaseURL {
		// Critic model left blank but points at the same server: inherit the primary model.
		c.Critic.LLM.Model = c.LLM.Model
	}
}

// Validate returns a non-nil error describing the first validation problem.
func (c *Config) Validate() error {
	// primary LLM
	if c.LLM.BaseURL == "" {
		return fmt.Errorf("llm.base_url is required")
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	// critic
	if c.Critic.LLM.BaseURL == "" {
		return fmt.Errorf("critic.llm.base_url is required")
	}
	if c.Critic.LLM.Model == "" {
		return fmt.Errorf("critic.llm.model is required")
	}
	if c.Critic.Decision != "llm" && c.Critic.Decision != "systemone" {
		return fmt.Errorf("critic.decision must be \"llm\" or \"systemone\", got %q", c.Critic.Decision)
	}
	if c.Critic.Decision == "systemone" && (c.Critic.Systemone.BaseURL == "" || c.Critic.Systemone.Path == "") {
		return fmt.Errorf("critic.systemone.base_url and critic.systemone.path are required when decision == \"systemone\"")
	}
	if c.Critic.Grammar != "" {
		if _, err := os.Stat(c.Critic.Grammar); err != nil {
			return fmt.Errorf("critic.grammar: %w", err)
		}
	}
	// tts
	if c.TTS.BaseURL == "" {
		return fmt.Errorf("tts.base_url is required")
	}
	if c.TTS.Model == "" {
		return fmt.Errorf("tts.model is required")
	}
	// search / sources
	switch c.Search.Provider {
	case "", "none":
		// no search provider — sources must come from arxiv/trusted URLs/docs
	case "brave", "tavily", "serper":
		if c.Search.APIKey == "" {
			return fmt.Errorf("search.api_key is required for provider %q", c.Search.Provider)
		}
	default:
		return fmt.Errorf("search.provider %q is not supported (want none|brave|tavily|serper)", c.Search.Provider)
	}
	if !c.Sources.Arxiv && c.Search.Provider == "none" && len(c.Sources.TrustedURLs) == 0 {
		return fmt.Errorf("enable at least one source: sources.arxiv, a search provider, or sources.trusted_urls")
	}
	// pipeline bounds
	if c.Pipeline.TargetMinutes < 15 || c.Pipeline.TargetMinutes > 20 {
		return fmt.Errorf("pipeline.target_minutes must be in [15, 20], got %d", c.Pipeline.TargetMinutes)
	}
	if c.Pipeline.MaxIterations < 1 {
		return fmt.Errorf("pipeline.max_iterations must be >= 1")
	}
	if c.Pipeline.PassThreshold <= 0 || c.Pipeline.PassThreshold > 1 {
		return fmt.Errorf("pipeline.pass_threshold must be in (0, 1]")
	}
	if c.Pipeline.WordsPerMinute < 80 || c.Pipeline.WordsPerMinute > 260 {
		return fmt.Errorf("pipeline.words_per_minute must be in [80, 260]")
	}
	if d, err := time.ParseDuration(c.Pipeline.TimeBudget); err != nil || d <= 0 {
		return fmt.Errorf("pipeline.time_budget must be a positive duration (e.g. \"10m\"): %v", err)
	}
	// prompts
	for name, file := range c.Prompts.Files {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("prompts.files[%q]: %w", name, err)
		}
	}
	return nil
}

// WordBudget returns the approximate word budget for the script.
func (c *Config) WordBudget() int {
	return c.Pipeline.TargetMinutes * c.Pipeline.WordsPerMinute
}

// TimeBudget parses the configured time budget.
func (c *Config) TimeBudgetDur() (time.Duration, error) {
	return time.ParseDuration(c.Pipeline.TimeBudget)
}

// Slug returns a filesystem-safe slug from a topic-ish string.
func Slug(topic string) string {
	s := strings.ToLower(topic)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
		} else {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "episode"
	}
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	return slug
}

// Snapshot is a JSON-friendly view of the config for meta.json.
func (c *Config) Snapshot() (map[string]any, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
