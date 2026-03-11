package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	gapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Email represents a parsed Gmail message.
type Email struct {
	ID         string
	From       string
	Subject    string
	Body       string
	ReceivedAt time.Time
}

// Client is a Gmail API client that authenticates via a GCP service account
// with domain-wide delegation, impersonating a target user.
type Client struct {
	service     *gapi.Service
	targetEmail string
}

// NewClient creates a new Gmail client using a GCP service account JSON file
// with domain-wide delegation. It impersonates the given targetEmail to access
// their mailbox.
func NewClient(serviceAccountPath, targetEmail string) (*Client, error) {
	keyBytes, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("reading service account key: %w", err)
	}

	jwtConfig, err := google.JWTConfigFromJSON(keyBytes, gapi.GmailReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("parsing service account key: %w", err)
	}

	// Set the Subject field to impersonate the target user via domain-wide delegation.
	jwtConfig.Subject = targetEmail

	ctx := context.Background()
	service, err := gapi.NewService(ctx, option.WithHTTPClient(jwtConfig.Client(ctx)))
	if err != nil {
		return nil, fmt.Errorf("creating gmail service: %w", err)
	}

	return &Client{
		service:     service,
		targetEmail: targetEmail,
	}, nil
}

// RegisterWatch sets up Gmail push notifications to the given Pub/Sub topic.
// Returns the history ID and expiration timestamp. Must be called at least every 7 days.
func (c *Client) RegisterWatch(ctx context.Context, projectID, topicName string) (uint64, int64, error) {
	topic := fmt.Sprintf("projects/%s/topics/%s", projectID, topicName)
	resp, err := c.service.Users.Watch(c.targetEmail, &gapi.WatchRequest{
		TopicName: topic,
		LabelIds:  []string{"INBOX"},
	}).Context(ctx).Do()
	if err != nil {
		return 0, 0, fmt.Errorf("registering gmail watch: %w", err)
	}
	return resp.HistoryId, resp.Expiration, nil
}

// FetchNewEmails retrieves new emails from the target inbox.
//
// If historyID is empty, it lists the 10 most recent messages.
// If historyID is set, it uses the Gmail History API to fetch only new messages
// since that history ID.
//
// It returns the parsed emails, the latest history ID for subsequent calls, and
// any error encountered.
func (c *Client) FetchNewEmails(ctx context.Context, historyID string) ([]Email, string, error) {
	if historyID == "" {
		return c.fetchRecent(ctx)
	}
	return c.fetchSinceHistory(ctx, historyID)
}

// fetchRecent lists the 10 most recent messages from the inbox.
func (c *Client) fetchRecent(ctx context.Context) ([]Email, string, error) {
	listResp, err := c.service.Users.Messages.List(c.targetEmail).
		MaxResults(10).
		LabelIds("INBOX").
		Context(ctx).
		Do()
	if err != nil {
		return nil, "", fmt.Errorf("listing messages: %w", err)
	}

	var emails []Email
	var latestHistoryID string

	for _, msg := range listResp.Messages {
		email, msgHistoryID, err := c.getMessage(ctx, msg.Id)
		if err != nil {
			return nil, "", fmt.Errorf("getting message %s: %w", msg.Id, err)
		}
		emails = append(emails, email)

		// Track the highest history ID across all fetched messages.
		if msgHistoryID > latestHistoryID {
			latestHistoryID = msgHistoryID
		}
	}

	// If no messages were found, get the current history ID from the profile.
	if latestHistoryID == "" {
		profile, err := c.service.Users.GetProfile(c.targetEmail).Context(ctx).Do()
		if err != nil {
			return nil, "", fmt.Errorf("getting profile: %w", err)
		}
		latestHistoryID = fmt.Sprintf("%d", profile.HistoryId)
	}

	return emails, latestHistoryID, nil
}

// fetchSinceHistory uses the Gmail History API to get messages added since the
// given history ID.
func (c *Client) fetchSinceHistory(ctx context.Context, historyID string) ([]Email, string, error) {
	historyIDNum, err := parseHistoryID(historyID)
	if err != nil {
		return nil, "", err
	}

	var emails []Email
	latestHistoryID := historyID
	seen := make(map[string]bool)

	pageToken := ""
	for {
		req := c.service.Users.History.List(c.targetEmail).
			StartHistoryId(historyIDNum).
			HistoryTypes("messageAdded").
			LabelId("INBOX").
			Context(ctx)

		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		resp, err := req.Do()
		if err != nil {
			return nil, "", fmt.Errorf("listing history: %w", err)
		}

		// Update to the latest history ID returned by the API.
		if resp.HistoryId > 0 {
			newID := fmt.Sprintf("%d", resp.HistoryId)
			if newID > latestHistoryID {
				latestHistoryID = newID
			}
		}

		for _, h := range resp.History {
			for _, added := range h.MessagesAdded {
				msgID := added.Message.Id
				if seen[msgID] {
					continue
				}
				seen[msgID] = true

				email, _, err := c.getMessage(ctx, msgID)
				if err != nil {
					return nil, "", fmt.Errorf("getting message %s: %w", msgID, err)
				}
				emails = append(emails, email)
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return emails, latestHistoryID, nil
}

// getMessage fetches a single message by ID and parses it into an Email.
// It returns the parsed email and the message's history ID.
func (c *Client) getMessage(ctx context.Context, messageID string) (Email, string, error) {
	msg, err := c.service.Users.Messages.Get(c.targetEmail, messageID).
		Format("full").
		Context(ctx).
		Do()
	if err != nil {
		return Email{}, "", fmt.Errorf("fetching message: %w", err)
	}

	email := Email{
		ID:         msg.Id,
		ReceivedAt: time.UnixMilli(msg.InternalDate),
	}

	for _, header := range msg.Payload.Headers {
		switch strings.ToLower(header.Name) {
		case "from":
			email.From = header.Value
		case "subject":
			email.Subject = header.Value
		}
	}

	email.Body = extractBody(msg.Payload)
	historyID := fmt.Sprintf("%d", msg.HistoryId)

	return email, historyID, nil
}

// extractBody walks the MIME parts tree and returns the plain text body.
// If no plain text part is found, it falls back to HTML.
func extractBody(payload *gapi.MessagePart) string {
	if payload == nil {
		return ""
	}

	// Single-part message.
	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		return decodeBody(payload.Body.Data)
	}

	// Walk multi-part structure, preferring text/plain.
	var htmlFallback string
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			return decodeBody(part.Body.Data)
		}
		if part.MimeType == "text/html" && part.Body != nil && part.Body.Data != "" {
			htmlFallback = decodeBody(part.Body.Data)
		}
		// Recurse into nested multipart structures.
		if len(part.Parts) > 0 {
			if body := extractBody(part); body != "" {
				return body
			}
		}
	}

	return htmlFallback
}

// decodeBody decodes a Gmail API URL-safe base64-encoded body string.
func decodeBody(data string) string {
	// Gmail API uses URL-safe base64 without padding.
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(data)
	if err != nil {
		return data
	}
	return string(decoded)
}

// parseHistoryID converts a history ID string to uint64.
func parseHistoryID(id string) (uint64, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid history ID %q: %w", id, err)
	}
	return n, nil
}
