package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hyagh/kivo/internal/config"
	"github.com/hyagh/kivo/internal/deploy"
	"github.com/hyagh/kivo/internal/gmail"
	"github.com/hyagh/kivo/internal/notion"
	"github.com/hyagh/kivo/internal/pipeline"
	"github.com/hyagh/kivo/internal/triage"
)

// pubSubPushMessage is the envelope structure for a Google Cloud Pub/Sub push delivery.
type pubSubPushMessage struct {
	Message struct {
		Data string `json:"data"`
		ID   string `json:"messageId"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// gmailNotification is the payload inside the base64-decoded Pub/Sub message data.
type gmailNotification struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    uint64 `json:"historyId"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// If GCP_SERVICE_ACCOUNT_B64 is set (for deployed environments), decode it
	// and write to a temp file for the Gmail client.
	saPath := cfg.GCP.ServiceAccountPath
	if saB64 := os.Getenv("GCP_SERVICE_ACCOUNT_B64"); saB64 != "" {
		saJSON, err := base64.StdEncoding.DecodeString(saB64)
		if err != nil {
			log.Fatalf("Failed to decode GCP_SERVICE_ACCOUNT_B64: %v", err)
		}
		tmpFile, err := os.CreateTemp("", "kivo-sa-*.json")
		if err != nil {
			log.Fatalf("Failed to create temp file for service account: %v", err)
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.Write(saJSON); err != nil {
			log.Fatalf("Failed to write service account to temp file: %v", err)
		}
		tmpFile.Close()
		saPath = tmpFile.Name()
		log.Println("Using service account from GCP_SERVICE_ACCOUNT_B64 env var")
	}

	gmailClient, err := gmail.NewClient(saPath, cfg.Gmail.TargetEmail)
	if err != nil {
		log.Fatalf("Failed to create Gmail client: %v", err)
	}

	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	triageEngine := triage.NewEngine(anthropicKey, cfg.Anthropic.TriageModel, cfg.Triage.ScoreThreshold)

	// Create the deployer if GitHub and Vercel tokens are configured.
	var deployer *deploy.Deployer
	if cfg.Deploy.GithubToken != "" && cfg.Deploy.VercelToken != "" {
		deployer = deploy.New(cfg.Deploy.GithubToken, cfg.Deploy.VercelToken, cfg.Deploy.GithubOrg)
		log.Println("Deploy integration enabled (GitHub + Vercel)")
	} else {
		log.Println("Deploy integration disabled (missing github_token or vercel_token)")
	}

	venturePipeline := pipeline.New(anthropicKey, cfg.Anthropic.BuildModel, deployer)

	var notionClient *notion.Client
	notionKey := os.Getenv("NOTION_API_KEY")
	if notionKey != "" && cfg.Notion.IdeasDatabaseID != "" {
		notionClient = notion.NewClient(notionKey, cfg.Notion.IdeasDatabaseID, cfg.Notion.VenturesDatabaseID)
		log.Println("Notion integration enabled")
	} else {
		log.Println("Notion integration disabled (missing API key or database IDs)")
	}

	handler := newWebhookHandler(cfg, gmailClient, triageEngine, venturePipeline, notionClient)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /webhook", handler.handleWebhook)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Auto-renew Gmail push registration every 6 days.
	go func() {
		ticker := time.NewTicker(6 * 24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			historyID, exp, err := gmailClient.RegisterWatch(
				context.Background(), cfg.GCP.ProjectID, cfg.GCP.PubSubTopic)
			if err != nil {
				log.Printf("Gmail watch renewal failed: %v", err)
				continue
			}
			log.Printf("Gmail watch renewed (historyId=%d, expiration=%d)", historyID, exp)
		}
	}()

	// Start server in a goroutine so we can handle shutdown signals.
	go func() {
		log.Printf("Receiver listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt or termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %v, shutting down gracefully...", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// webhookHandler holds the dependencies for the webhook endpoint.
type webhookHandler struct {
	cfg             *config.Config
	gmailClient     *gmail.Client
	triageEngine    *triage.Engine
	venturePipeline *pipeline.Pipeline
	notionClient    *notion.Client

	mu            sync.Mutex
	lastHistoryID string
}

func newWebhookHandler(cfg *config.Config, gmailClient *gmail.Client, triageEngine *triage.Engine, venturePipeline *pipeline.Pipeline, notionClient *notion.Client) *webhookHandler {
	return &webhookHandler{
		cfg:             cfg,
		gmailClient:     gmailClient,
		triageEngine:    triageEngine,
		venturePipeline: venturePipeline,
		notionClient:    notionClient,
		lastHistoryID:   cfg.Gmail.LastHistoryID,
	}
}

// handleHealth responds with a simple health check.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

// handleWebhook processes incoming Google Cloud Pub/Sub push messages.
func (h *webhookHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Verify the Pub/Sub token if one is configured.
	if h.cfg.Server.PubSubVerifyToken != "" {
		token := r.URL.Query().Get("token")
		if token != h.cfg.Server.PubSubVerifyToken {
			log.Printf("Webhook rejected: invalid verify token")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	var pushMsg pubSubPushMessage
	if err := json.NewDecoder(r.Body).Decode(&pushMsg); err != nil {
		log.Printf("Webhook error: failed to decode push message: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Decode the base64-encoded notification payload.
	data, err := base64.StdEncoding.DecodeString(pushMsg.Message.Data)
	if err != nil {
		log.Printf("Webhook error: failed to decode message data: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var notification gmailNotification
	if err := json.Unmarshal(data, &notification); err != nil {
		log.Printf("Webhook error: failed to parse notification: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Printf("Received Gmail notification for %s (historyId: %d)",
		notification.EmailAddress, notification.HistoryID)

	// Use the tracked history ID for incremental fetching.
	h.mu.Lock()
	currentHistoryID := h.lastHistoryID
	h.mu.Unlock()

	ctx := r.Context()
	emails, newHistoryID, err := h.gmailClient.FetchNewEmails(ctx, currentHistoryID)
	if err != nil {
		log.Printf("Webhook error: failed to fetch emails: %v", err)
		// Return 200 to prevent Pub/Sub from retrying on transient errors.
		// The next push notification will retry the fetch with the same history ID.
		w.WriteHeader(http.StatusOK)
		return
	}

	// Update the tracked history ID.
	h.mu.Lock()
	h.lastHistoryID = newHistoryID
	h.mu.Unlock()

	if len(emails) == 0 {
		log.Printf("No new emails since historyId %s", currentHistoryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Process each email through triage → pipeline asynchronously.
	for _, email := range emails {
		log.Printf("New email [%s] from=%q subject=%q", email.ID, email.From, email.Subject)
		go h.processIdea(email)
	}

	w.WriteHeader(http.StatusOK)
}

// processIdea runs the full idea-to-venture pipeline for a single email.
func (h *webhookHandler) processIdea(email gmail.Email) {
	ctx := context.Background()

	// Stage 1: Triage — score the idea with Haiku.
	score, err := h.triageEngine.ScoreIdea(ctx, email.Subject, email.Body)
	if err != nil {
		log.Printf("[%s] Triage failed: %v", email.ID, err)
		return
	}

	log.Printf("[%s] Triage: score=%d product=%q pursue=%v",
		email.ID, score.Score, score.ProductName, score.ShouldPursue)

	// Save idea to Notion regardless of score.
	if h.notionClient != nil {
		if err := h.notionClient.SaveIdea(ctx, notion.IdeaRecord{
			Subject:      email.Subject,
			Score:        score.Score,
			MarketSize:   score.MarketSize,
			Feasibility:  score.Feasibility,
			ProductName:  score.ProductName,
			Summary:      score.Summary,
			ShouldPursue: score.ShouldPursue,
			EmailID:      email.ID,
		}); err != nil {
			log.Printf("[%s] Notion save idea failed: %v", email.ID, err)
		}
	}

	// Gate: only continue if idea passes threshold.
	if !score.ShouldPursue {
		log.Printf("[%s] Idea below threshold (%d < %d), skipping pipeline",
			email.ID, score.Score, h.cfg.Triage.ScoreThreshold)
		return
	}

	// Stage 2: Run the venture pipeline (research → build → deploy → market).
	log.Printf("[%s] Starting venture pipeline for %q", email.ID, score.ProductName)
	venture, err := h.venturePipeline.Run(ctx, score.ProductName, score.Summary)
	if err != nil {
		log.Printf("[%s] Pipeline failed at stage %s: %v", email.ID, venture.Stage, err)
	}

	// Save venture to Notion.
	if h.notionClient != nil && venture != nil {
		tagline := ""
		targetAudience := ""
		valueProposition := ""
		if venture.Research != nil {
			tagline = venture.Research.Tagline
			targetAudience = venture.Research.TargetAudience
			valueProposition = venture.Research.ValueProposition
		}

		siteURL := ""
		repoURL := ""
		if venture.Deploy != nil {
			siteURL = venture.Deploy.SiteURL
		}
		if venture.Build != nil {
			repoURL = venture.Build.RepoURL
		}

		pageID, err := h.notionClient.SaveVenture(ctx, notion.VentureRecord{
			ProductName:      venture.ProductName,
			Stage:            string(venture.Stage),
			SiteURL:          siteURL,
			RepoURL:          repoURL,
			Tagline:          tagline,
			TargetAudience:   targetAudience,
			ValueProposition: valueProposition,
			Score:            score.Score,
		})
		if err != nil {
			log.Printf("[%s] Notion save venture failed: %v", email.ID, err)
		} else {
			log.Printf("[%s] Venture saved to Notion (page: %s)", email.ID, pageID)
		}
	}

	log.Printf("[%s] Pipeline complete for %q — stage: %s", email.ID, score.ProductName, venture.Stage)
}
