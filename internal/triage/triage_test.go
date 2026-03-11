package triage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeLLMResponse returns a valid Anthropic Messages API response containing
// the given JSON text as the model's output.
func fakeLLMResponse(text string) string {
	resp := anthropicResponse{
		Content: []contentBlock{
			{Type: "text", Text: text},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func TestScoreIdea_ParsesLLMOutput(t *testing.T) {
	llmJSON := `{
		"score": 82,
		"market_size": "large",
		"feasibility": "moderate",
		"uniqueness": "novel",
		"summary": "AI-powered pet food delivery optimized for dietary needs.",
		"product_name": "PawPantry",
		"reasoning": "Large pet market with clear willingness to pay. Moderate build complexity due to dietary recommendation engine. Novel angle combining AI personalization with pet nutrition."
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeLLMResponse(llmJSON)))
	}))
	defer server.Close()

	engine := NewEngine("test-key", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	score, err := engine.ScoreIdea(context.Background(), "Pet food AI", "An AI-powered pet food delivery service.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if score.Score != 82 {
		t.Errorf("Score = %d, want 82", score.Score)
	}
	if score.MarketSize != "large" {
		t.Errorf("MarketSize = %q, want %q", score.MarketSize, "large")
	}
	if score.Feasibility != "moderate" {
		t.Errorf("Feasibility = %q, want %q", score.Feasibility, "moderate")
	}
	if score.Uniqueness != "novel" {
		t.Errorf("Uniqueness = %q, want %q", score.Uniqueness, "novel")
	}
	if score.ProductName != "PawPantry" {
		t.Errorf("ProductName = %q, want %q", score.ProductName, "PawPantry")
	}
	if !score.ShouldPursue {
		t.Error("ShouldPursue = false, want true (score 82 >= threshold 70)")
	}
	if score.Subject != "Pet food AI" {
		t.Errorf("Subject = %q, want %q", score.Subject, "Pet food AI")
	}
	if score.RawIdea != "An AI-powered pet food delivery service." {
		t.Errorf("RawIdea = %q, want original body", score.RawIdea)
	}
	if score.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestScoreIdea_BelowThreshold(t *testing.T) {
	llmJSON := `{
		"score": 35,
		"market_size": "small",
		"feasibility": "hard",
		"uniqueness": "commodity",
		"summary": "Yet another todo app.",
		"product_name": "TodoAgain",
		"reasoning": "Extremely crowded market. No differentiation. Difficult to monetize."
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeLLMResponse(llmJSON)))
	}))
	defer server.Close()

	engine := NewEngine("test-key", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	score, err := engine.ScoreIdea(context.Background(), "Todo app", "A simple todo list application.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if score.ShouldPursue {
		t.Error("ShouldPursue = true, want false (score 35 < threshold 70)")
	}
	if score.Score != 35 {
		t.Errorf("Score = %d, want 35", score.Score)
	}
}

func TestScoreIdea_RequestStructure(t *testing.T) {
	var capturedReq anthropicRequest
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedReq)

		llmJSON := `{"score":50,"market_size":"medium","feasibility":"moderate","uniqueness":"differentiated","summary":"Test.","product_name":"Test","reasoning":"Test."}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeLLMResponse(llmJSON)))
	}))
	defer server.Close()

	engine := NewEngine("sk-test-123", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	_, err := engine.ScoreIdea(context.Background(), "Idea subject", "Idea body text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify headers.
	if got := capturedHeaders.Get("x-api-key"); got != "sk-test-123" {
		t.Errorf("x-api-key = %q, want %q", got, "sk-test-123")
	}
	if got := capturedHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
	}
	if got := capturedHeaders.Get("content-type"); got != "application/json" {
		t.Errorf("content-type = %q, want %q", got, "application/json")
	}

	// Verify request body structure.
	if capturedReq.Model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q, want %q", capturedReq.Model, "claude-haiku-4-5-20251001")
	}
	if capturedReq.MaxTokens != 1024 {
		t.Errorf("max_tokens = %d, want 1024", capturedReq.MaxTokens)
	}
	if capturedReq.System == "" {
		t.Error("system prompt should not be empty")
	}
	if len(capturedReq.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("message role = %q, want %q", capturedReq.Messages[0].Role, "user")
	}

	wantContent := "Subject: Idea subject\n\nIdea body text"
	if capturedReq.Messages[0].Content != wantContent {
		t.Errorf("message content = %q, want %q", capturedReq.Messages[0].Content, wantContent)
	}
}

func TestScoreIdea_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`))
	}))
	defer server.Close()

	engine := NewEngine("test-key", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	_, err := engine.ScoreIdea(context.Background(), "Test", "Test body")
	if err == nil {
		t.Fatal("expected error for 429 response, got nil")
	}
}

func TestScoreIdea_MalformedLLMOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// LLM returns invalid JSON.
		_, _ = w.Write([]byte(fakeLLMResponse("this is not json")))
	}))
	defer server.Close()

	engine := NewEngine("test-key", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	_, err := engine.ScoreIdea(context.Background(), "Test", "Test body")
	if err == nil {
		t.Fatal("expected error for malformed LLM output, got nil")
	}
}

func TestScoreIdea_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer server.Close()

	engine := NewEngine("test-key", "claude-haiku-4-5-20251001", 70)
	engine.apiURL = server.URL

	_, err := engine.ScoreIdea(context.Background(), "Test", "Test body")
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestScoreIdea_ThresholdBoundary(t *testing.T) {
	tests := []struct {
		name      string
		score     int
		threshold int
		want      bool
	}{
		{"exactly at threshold", 70, 70, true},
		{"one below threshold", 69, 70, false},
		{"one above threshold", 71, 70, true},
		{"zero score", 0, 70, false},
		{"perfect score", 100, 70, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmJSON, _ := json.Marshal(llmOutput{
				Score:       tt.score,
				MarketSize:  "medium",
				Feasibility: "moderate",
				Uniqueness:  "differentiated",
				Summary:     "Test idea.",
				ProductName: "TestProduct",
				Reasoning:   "Test reasoning.",
			})

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(fakeLLMResponse(string(llmJSON))))
			}))
			defer server.Close()

			engine := NewEngine("test-key", "claude-haiku-4-5-20251001", tt.threshold)
			engine.apiURL = server.URL

			result, err := engine.ScoreIdea(context.Background(), "Test", "Test body")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ShouldPursue != tt.want {
				t.Errorf("ShouldPursue = %v, want %v (score=%d, threshold=%d)",
					result.ShouldPursue, tt.want, tt.score, tt.threshold)
			}
		})
	}
}
