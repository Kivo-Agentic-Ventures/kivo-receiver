// Package notion provides a client for storing ideas and ventures
// in Notion databases using the Notion API directly.
package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const notionVersion = "2022-06-28"

var baseURL = "https://api.notion.com/v1"

// IdeaRecord holds the data for an idea to be saved in Notion.
type IdeaRecord struct {
	Subject      string
	Score        int
	MarketSize   string
	Feasibility  string
	ProductName  string
	Summary      string
	ShouldPursue bool
	EmailID      string
}

// VentureRecord holds the data for a venture to be saved in Notion.
type VentureRecord struct {
	ProductName      string
	Stage            string
	SiteURL          string
	RepoURL          string
	Tagline          string
	TargetAudience   string
	ValueProposition string
	Score            int
}

// VentureUpdate holds optional fields for updating an existing venture.
// Nil pointer fields are omitted from the update.
type VentureUpdate struct {
	Stage   *string
	SiteURL *string
	RepoURL *string
}

// Client interacts with the Notion API to manage ideas and ventures databases.
type Client struct {
	apiKey             string
	ideasDatabaseID    string
	venturesDatabaseID string
	httpClient         *http.Client
}

// NewClient creates a new Notion API client.
func NewClient(apiKey, ideasDBID, venturesDBID string) *Client {
	return &Client{
		apiKey:             apiKey,
		ideasDatabaseID:    ideasDBID,
		venturesDatabaseID: venturesDBID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SaveIdea creates a page in the Ideas database with the given idea data.
func (c *Client) SaveIdea(ctx context.Context, idea IdeaRecord) error {
	properties := map[string]any{
		"Idea":          titleProperty(idea.Subject),
		"Score":         numberProperty(float64(idea.Score)),
		"Market Size":   selectProperty(idea.MarketSize),
		"Feasibility":   selectProperty(idea.Feasibility),
		"Product Name":  richTextProperty(idea.ProductName),
		"Summary":       richTextProperty(idea.Summary),
		"Should Pursue": checkboxProperty(idea.ShouldPursue),
		"Email ID":      richTextProperty(idea.EmailID),
	}

	body := map[string]any{
		"parent":     map[string]string{"database_id": c.ideasDatabaseID},
		"properties": properties,
	}

	if _, err := c.createPage(ctx, body); err != nil {
		return fmt.Errorf("saving idea %q: %w", idea.Subject, err)
	}
	return nil
}

// SaveVenture creates a page in the Ventures database and returns the page ID.
func (c *Client) SaveVenture(ctx context.Context, venture VentureRecord) (string, error) {
	properties := map[string]any{
		"Product Name":      titleProperty(venture.ProductName),
		"Stage":             selectProperty(venture.Stage),
		"Tagline":           richTextProperty(venture.Tagline),
		"Target Audience":   richTextProperty(venture.TargetAudience),
		"Value Proposition": richTextProperty(venture.ValueProposition),
		"Score":             numberProperty(float64(venture.Score)),
	}

	if venture.SiteURL != "" {
		properties["Site URL"] = urlProperty(venture.SiteURL)
	}
	if venture.RepoURL != "" {
		properties["Repo URL"] = urlProperty(venture.RepoURL)
	}

	body := map[string]any{
		"parent":     map[string]string{"database_id": c.venturesDatabaseID},
		"properties": properties,
	}

	pageID, err := c.createPage(ctx, body)
	if err != nil {
		return "", fmt.Errorf("saving venture %q: %w", venture.ProductName, err)
	}
	return pageID, nil
}

// UpdateVenture patches an existing venture page with the provided updates.
// Only non-nil fields in VentureUpdate are sent to the API.
func (c *Client) UpdateVenture(ctx context.Context, pageID string, updates VentureUpdate) error {
	properties := make(map[string]any)

	if updates.Stage != nil {
		properties["Stage"] = selectProperty(*updates.Stage)
	}
	if updates.SiteURL != nil {
		properties["Site URL"] = urlProperty(*updates.SiteURL)
	}
	if updates.RepoURL != nil {
		properties["Repo URL"] = urlProperty(*updates.RepoURL)
	}

	body := map[string]any{
		"properties": properties,
	}

	if err := c.updatePage(ctx, pageID, body); err != nil {
		return fmt.Errorf("updating venture %q: %w", pageID, err)
	}
	return nil
}

// --- internal HTTP helpers ---

// createPage POSTs a new page to the Notion API and returns the created page ID.
func (c *Client) createPage(ctx context.Context, body map[string]any) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/pages", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", readAPIError(resp)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	return result.ID, nil
}

// updatePage PATCHes an existing page in the Notion API.
func (c *Client) updatePage(ctx context.Context, pageID string, body map[string]any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	url := fmt.Sprintf("%s/pages/%s", baseURL, pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readAPIError(resp)
	}
	return nil
}

// setHeaders applies the required Notion API headers to a request.
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Content-Type", "application/json")
}

// readAPIError extracts a meaningful error from a non-200 Notion API response.
func readAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("notion api status %d (failed to read body: %w)", resp.StatusCode, err)
	}

	var apiErr struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		return fmt.Errorf("notion api error %d (%s): %s", resp.StatusCode, apiErr.Code, apiErr.Message)
	}

	return fmt.Errorf("notion api error %d: %s", resp.StatusCode, string(body))
}

// --- Notion property builders ---

func titleProperty(text string) map[string]any {
	return map[string]any{
		"title": []map[string]any{
			{"text": map[string]string{"content": text}},
		},
	}
}

func richTextProperty(text string) map[string]any {
	return map[string]any{
		"rich_text": []map[string]any{
			{"text": map[string]string{"content": text}},
		},
	}
}

func numberProperty(value float64) map[string]any {
	return map[string]any{
		"number": value,
	}
}

func selectProperty(name string) map[string]any {
	return map[string]any{
		"select": map[string]string{"name": name},
	}
}

func checkboxProperty(checked bool) map[string]any {
	return map[string]any{
		"checkbox": checked,
	}
}

func dateProperty(iso string) map[string]any {
	return map[string]any{
		"date": map[string]string{"start": iso},
	}
}

func urlProperty(link string) map[string]any {
	return map[string]any{
		"url": link,
	}
}
