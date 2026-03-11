package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	content := `
company:
  name: "Test Corp"
  email: "test@example.com"
  domain: "example.com"

gcp:
  project_id: "test-project"
  service_account_path: "${TEST_SA_PATH}/sa.json"
  pubsub_topic: "test-topic"
  pubsub_subscription: "test-sub"

gmail:
  target_email: "inbox@example.com"

anthropic:
  triage_model: "claude-haiku-4-5-20251001"
  research_model: "claude-sonnet-4-6"
  build_model: "claude-sonnet-4-6"

triage:
  score_threshold: 60
  auto_approve_threshold: 90

deploy:
  platform: "cloudflare"
  vercel_token: "${TEST_VERCEL_TOKEN}"
  github_token: "${TEST_GITHUB_TOKEN}"
  github_org: "testorg"

notion:
  ideas_database_id: "abc123"
  ventures_database_id: "def456"

server:
  port: 9090
  pubsub_verify_token: "${TEST_PUBSUB_TOKEN}"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_SA_PATH", "/opt/keys")
	t.Setenv("TEST_VERCEL_TOKEN", "vtoken123")
	t.Setenv("TEST_GITHUB_TOKEN", "ghtoken456")
	t.Setenv("TEST_PUBSUB_TOKEN", "pstoken789")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Company
	assertEqual(t, "company.name", "Test Corp", cfg.Company.Name)
	assertEqual(t, "company.email", "test@example.com", cfg.Company.Email)

	// GCP — env var expansion
	assertEqual(t, "gcp.service_account_path", "/opt/keys/sa.json", cfg.GCP.ServiceAccountPath)
	assertEqual(t, "gcp.pubsub_topic", "test-topic", cfg.GCP.PubSubTopic)

	// Triage — integer fields
	assertEqualInt(t, "triage.score_threshold", 60, cfg.Triage.ScoreThreshold)
	assertEqualInt(t, "triage.auto_approve_threshold", 90, cfg.Triage.AutoApproveThreshold)

	// Deploy — env var expansion
	assertEqual(t, "deploy.platform", "cloudflare", cfg.Deploy.Platform)
	assertEqual(t, "deploy.vercel_token", "vtoken123", cfg.Deploy.VercelToken)
	assertEqual(t, "deploy.github_token", "ghtoken456", cfg.Deploy.GithubToken)

	// Server
	assertEqualInt(t, "server.port", 9090, cfg.Server.Port)
	assertEqual(t, "server.pubsub_verify_token", "pstoken789", cfg.Server.PubSubVerifyToken)

	// Notion
	assertEqual(t, "notion.ideas_database_id", "abc123", cfg.Notion.IdeasDatabaseID)
}

func TestLoadDefaults(t *testing.T) {
	// Minimal config — everything should fall back to defaults.
	content := `
company:
  name: "Minimal"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	assertEqualInt(t, "server.port", 8080, cfg.Server.Port)
	assertEqualInt(t, "triage.score_threshold", 70, cfg.Triage.ScoreThreshold)
	assertEqual(t, "deploy.platform", "vercel", cfg.Deploy.Platform)
	assertEqual(t, "anthropic.triage_model", "claude-haiku-4-5-20251001", cfg.Anthropic.TriageModel)
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestExpandEnvVarsUnset(t *testing.T) {
	// Unset variables should expand to empty string.
	t.Setenv("KIVO_TEST_SET", "hello")
	result := expandEnvVars("a=${KIVO_TEST_SET} b=${KIVO_TEST_UNSET_12345}")
	expected := "a=hello b="
	if result != expected {
		t.Errorf("expandEnvVars: got %q, want %q", result, expected)
	}
}

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}

func assertEqualInt(t *testing.T, field string, want, got int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %d, want %d", field, got, want)
	}
}
