package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/hyagh/kivo/internal/config"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	saJSON, err := os.ReadFile(cfg.GCP.ServiceAccountPath)
	if err != nil {
		log.Fatalf("failed to read service account: %v", err)
	}

	jwtConfig, err := google.JWTConfigFromJSON(saJSON, gmail.GmailReadonlyScope)
	if err != nil {
		log.Fatalf("failed to parse service account: %v", err)
	}
	jwtConfig.Subject = cfg.Gmail.TargetEmail

	ctx := context.Background()
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(jwtConfig.Client(ctx)))
	if err != nil {
		log.Fatalf("failed to create Gmail service: %v", err)
	}

	topicName := fmt.Sprintf("projects/%s/topics/%s", cfg.GCP.ProjectID, cfg.GCP.PubSubTopic)

	req := &gmail.WatchRequest{
		TopicName: topicName,
		LabelIds:  []string{"INBOX"},
	}

	resp, err := svc.Users.Watch(cfg.Gmail.TargetEmail, req).Do()
	if err != nil {
		log.Fatalf("failed to register Gmail push: %v", err)
	}

	fmt.Printf("Gmail push registered successfully!\n")
	fmt.Printf("  History ID: %d\n", resp.HistoryId)
	fmt.Printf("  Expiration: %d\n", resp.Expiration)
	fmt.Printf("\nNote: Gmail push expires after ~7 days. Set up a cron to re-register.\n")
}
