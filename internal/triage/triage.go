package triage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// IdeaScore holds the structured evaluation of a startup idea produced by
// the triage LLM. A score of 0-100 captures overall viability.
type IdeaScore struct {
	EmailID      string    `json:"email_id"`
	Subject      string    `json:"subject"`
	RawIdea      string    `json:"raw_idea"`
	Score        int       `json:"score"`         // 0-100
	MarketSize   string    `json:"market_size"`   // "small", "medium", "large", "massive"
	Feasibility  string    `json:"feasibility"`   // "easy", "moderate", "hard", "very_hard"
	Uniqueness   string    `json:"uniqueness"`    // "commodity", "differentiated", "novel", "breakthrough"
	Summary      string    `json:"summary"`       // 1-2 sentence summary of the idea
	ProductName  string    `json:"product_name"`  // suggested product name
	Reasoning    string    `json:"reasoning"`     // why this score
	ShouldPursue bool      `json:"should_pursue"`
	CreatedAt    time.Time `json:"created_at"`
}

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"

const systemPrompt = `You are a startup idea evaluator for a venture studio. Analyze the startup idea provided and return a JSON evaluation.

Evaluate the idea across these dimensions:
1. **Market Size** — How large is the addressable market? Consider TAM, growth trends, and existing demand signals.
2. **Technical Feasibility** — How hard is it to build an MVP? Consider required infrastructure, team size, and time to market.
3. **Uniqueness** — How differentiated is this from existing solutions? Consider competitive landscape and defensibility.
4. **Competitive Landscape** — Are there dominant incumbents? Is there room for a new entrant?
5. **Monetization Potential** — Is there a clear path to revenue? Consider willingness to pay and unit economics.

Return ONLY a JSON object with exactly these fields (no markdown, no wrapping):
{
  "score": <integer 0-100, overall viability score>,
  "market_size": <"small" | "medium" | "large" | "massive">,
  "feasibility": <"easy" | "moderate" | "hard" | "very_hard">,
  "uniqueness": <"commodity" | "differentiated" | "novel" | "breakthrough">,
  "summary": <string, 1-2 sentence summary of the idea>,
  "product_name": <string, a catchy suggested product name>,
  "reasoning": <string, 2-3 sentences explaining the score>
}

Scoring guidelines:
- 0-30: Not viable — weak market, high difficulty, or crowded space
- 31-50: Below average — some merit but significant challenges
- 51-70: Promising — worth monitoring but needs refinement
- 71-85: Strong — clear market, feasible build, good differentiation
- 86-100: Exceptional — large market, easy to build, highly unique`

// Engine evaluates startup ideas by calling the Anthropic Messages API.
type Engine struct {
	apiKey    string
	model     string
	threshold int
	client    *http.Client
	apiURL    string
}

// NewEngine creates an Engine that scores ideas using the given Anthropic model.
// Ideas scoring at or above threshold are marked as worth pursuing.
func NewEngine(apiKey, model string, threshold int) *Engine {
	return &Engine{
		apiKey:    apiKey,
		model:     model,
		threshold: threshold,
		client:    http.DefaultClient,
		apiURL:    anthropicAPIURL,
	}
}

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse captures the relevant fields from the Messages API response.
type anthropicResponse struct {
	Content []contentBlock `json:"content"`
	Error   *apiError      `json:"error,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// llmOutput mirrors the JSON structure returned by the LLM (a subset of IdeaScore).
type llmOutput struct {
	Score       int    `json:"score"`
	MarketSize  string `json:"market_size"`
	Feasibility string `json:"feasibility"`
	Uniqueness  string `json:"uniqueness"`
	Summary     string `json:"summary"`
	ProductName string `json:"product_name"`
	Reasoning   string `json:"reasoning"`
}

// ScoreIdea sends the idea described by subject and body to the Anthropic
// Messages API and returns a structured IdeaScore.
func (e *Engine) ScoreIdea(ctx context.Context, subject, body string) (*IdeaScore, error) {
	userContent := fmt.Sprintf("Subject: %s\n\n%s", subject, body)

	reqBody := anthropicRequest{
		Model:     e.model,
		MaxTokens: 1024,
		System:    systemPrompt,
		Messages: []message{
			{Role: "user", Content: userContent},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("triage: marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("triage: creating request: %w", err)
	}
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("triage: calling anthropic API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("triage: reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("triage: API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("triage: parsing API response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("triage: API error (%s): %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("triage: empty content in API response")
	}

	rawText := cleanJSON(apiResp.Content[0].Text)

	var output llmOutput
	if err := json.Unmarshal([]byte(rawText), &output); err != nil {
		return nil, fmt.Errorf("triage: parsing LLM JSON output: %w (raw: %s)", err, apiResp.Content[0].Text)
	}

	score := &IdeaScore{
		Subject:      subject,
		RawIdea:      body,
		Score:        output.Score,
		MarketSize:   output.MarketSize,
		Feasibility:  output.Feasibility,
		Uniqueness:   output.Uniqueness,
		Summary:      output.Summary,
		ProductName:  output.ProductName,
		Reasoning:    output.Reasoning,
		ShouldPursue: output.Score >= e.threshold,
		CreatedAt:    time.Now(),
	}

	return score, nil
}

// cleanJSON strips markdown code fences and leading/trailing whitespace that
// LLMs sometimes wrap around JSON responses.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}
