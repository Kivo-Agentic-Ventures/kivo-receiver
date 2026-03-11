package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for the Kivo application.
type Config struct {
	Company   Company   `yaml:"company"`
	GCP       GCP       `yaml:"gcp"`
	Gmail     Gmail     `yaml:"gmail"`
	Anthropic Anthropic `yaml:"anthropic"`
	Triage    Triage    `yaml:"triage"`
	Deploy    Deploy    `yaml:"deploy"`
	Notion    Notion    `yaml:"notion"`
	Server    Server    `yaml:"server"`
}

type Company struct {
	Name   string `yaml:"name"`
	Email  string `yaml:"email"`
	Domain string `yaml:"domain"`
}

type GCP struct {
	ProjectID          string `yaml:"project_id"`
	ServiceAccountPath string `yaml:"service_account_path"`
	PubSubTopic        string `yaml:"pubsub_topic"`
	PubSubSubscription string `yaml:"pubsub_subscription"`
}

type Gmail struct {
	TargetEmail   string `yaml:"target_email"`
	LastHistoryID string `yaml:"last_history_id"`
}

type Anthropic struct {
	TriageModel   string `yaml:"triage_model"`
	ResearchModel string `yaml:"research_model"`
	BuildModel    string `yaml:"build_model"`
}

type Triage struct {
	ScoreThreshold       int `yaml:"score_threshold"`
	AutoApproveThreshold int `yaml:"auto_approve_threshold"`
}

type Deploy struct {
	Platform    string `yaml:"platform"`
	VercelToken string `yaml:"vercel_token"`
	GithubToken string `yaml:"github_token"`
	GithubOrg   string `yaml:"github_org"`
}

type Notion struct {
	IdeasDatabaseID    string `yaml:"ideas_database_id"`
	VenturesDatabaseID string `yaml:"ventures_database_id"`
}

type Server struct {
	Port              int    `yaml:"port"`
	PubSubVerifyToken string `yaml:"pubsub_verify_token"`
}

// Load reads the YAML configuration from the given file path, expands
// environment variables in string values, and returns the parsed Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand ${VAR} references before parsing YAML.
	expanded := expandEnvVars(string(data))

	cfg := defaults()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

// envVarPattern matches ${VAR_NAME} references in strings.
var envVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// expandEnvVars replaces all ${VAR} occurrences with the corresponding
// environment variable value. Unset variables expand to an empty string.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := envVarPattern.FindStringSubmatch(match)[1]
		return os.Getenv(varName)
	})
}

// defaults returns a Config populated with sensible default values.
func defaults() *Config {
	return &Config{
		Anthropic: Anthropic{
			TriageModel:   "claude-haiku-4-5-20251001",
			ResearchModel: "claude-sonnet-4-6",
			BuildModel:    "claude-sonnet-4-6",
		},
		Triage: Triage{
			ScoreThreshold:       70,
			AutoApproveThreshold: 85,
		},
		Deploy: Deploy{
			Platform: "vercel",
		},
		Server: Server{
			Port: 8080,
		},
	}
}
