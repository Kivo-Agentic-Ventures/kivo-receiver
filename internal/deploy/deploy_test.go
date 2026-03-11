package deploy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- CreateRepo tests ---

func TestCreateRepo_Success(t *testing.T) {
	var capturedReq createRepoRequest
	var capturedHeaders http.Header
	var capturedMethod string
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedHeaders = r.Header.Clone()

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createRepoResponse{
			HTMLURL:  "https://github.com/withkivo/pawpantry",
			FullName: "withkivo/pawpantry",
		})
	}))
	defer srv.Close()

	d := New("gh-token-123", "vc-token-456", "withkivo")
	d.GitHubBaseURL = srv.URL

	repoURL, err := d.CreateRepo(context.Background(), "pawpantry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repoURL != "https://github.com/withkivo/pawpantry" {
		t.Errorf("repoURL = %q, want %q", repoURL, "https://github.com/withkivo/pawpantry")
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedPath != "/orgs/withkivo/repos" {
		t.Errorf("path = %q, want /orgs/withkivo/repos", capturedPath)
	}
	if capturedReq.Name != "pawpantry" {
		t.Errorf("name = %q, want %q", capturedReq.Name, "pawpantry")
	}
	if capturedReq.Private {
		t.Error("private = true, want false")
	}
	if capturedHeaders.Get("Authorization") != "Bearer gh-token-123" {
		t.Errorf("Authorization = %q, want %q", capturedHeaders.Get("Authorization"), "Bearer gh-token-123")
	}
	if capturedHeaders.Get("X-GitHub-Api-Version") != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", capturedHeaders.Get("X-GitHub-Api-Version"), "2022-11-28")
	}
}

func TestCreateRepo_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"}]}`))
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token", "withkivo")
	d.GitHubBaseURL = srv.URL

	_, err := d.CreateRepo(context.Background(), "existing-repo")
	if err == nil {
		t.Fatal("expected error for 422 response, got nil")
	}
}

// --- PushFiles tests ---

func TestPushFiles_Success(t *testing.T) {
	var requests []struct {
		method string
		path   string
		body   putContentsRequest
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req putContentsRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		requests = append(requests, struct {
			method string
			path   string
			body   putContentsRequest
		}{
			method: r.Method,
			path:   r.URL.Path,
			body:   req,
		})

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"content":{"sha":"abc123"}}`))
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token", "withkivo")
	d.GitHubBaseURL = srv.URL

	files := map[string]string{
		"index.html": "<html><body>Hello</body></html>",
	}

	err := d.PushFiles(context.Background(), "pawpantry", files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	req := requests[0]
	if req.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", req.method)
	}
	if req.path != "/repos/withkivo/pawpantry/contents/index.html" {
		t.Errorf("path = %q, want /repos/withkivo/pawpantry/contents/index.html", req.path)
	}

	// Verify content is base64-encoded.
	decoded, err := base64.StdEncoding.DecodeString(req.body.Content)
	if err != nil {
		t.Fatalf("content is not valid base64: %v", err)
	}
	if string(decoded) != "<html><body>Hello</body></html>" {
		t.Errorf("decoded content = %q, want HTML", string(decoded))
	}
	if req.body.Message != "Add index.html" {
		t.Errorf("message = %q, want %q", req.body.Message, "Add index.html")
	}
}

func TestPushFiles_MultipleFiles(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"content":{"sha":"abc"}}`))
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token", "withkivo")
	d.GitHubBaseURL = srv.URL

	files := map[string]string{
		"index.html":  "<html>Landing</html>",
		"style.css":   "body { margin: 0; }",
		"package.json": `{"name":"test"}`,
	}

	err := d.PushFiles(context.Background(), "venture", files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}
}

func TestPushFiles_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token", "withkivo")
	d.GitHubBaseURL = srv.URL

	err := d.PushFiles(context.Background(), "nonexistent", map[string]string{
		"index.html": "<html></html>",
	})
	if err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// --- DeployToVercel tests ---

func TestDeployToVercel_Success(t *testing.T) {
	var capturedReq vercelDeployRequest
	var capturedHeaders http.Header
	var capturedMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedHeaders = r.Header.Clone()

		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(vercelDeployResponse{
			ID:  "dpl_abc123",
			URL: "pawpantry-withkivo.vercel.app",
		})
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token-789", "withkivo")
	d.VercelBaseURL = srv.URL

	siteURL, err := d.DeployToVercel(context.Background(), "pawpantry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if siteURL != "https://pawpantry-withkivo.vercel.app" {
		t.Errorf("siteURL = %q, want %q", siteURL, "https://pawpantry-withkivo.vercel.app")
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", capturedMethod)
	}
	if capturedHeaders.Get("Authorization") != "Bearer vc-token-789" {
		t.Errorf("Authorization = %q, want %q", capturedHeaders.Get("Authorization"), "Bearer vc-token-789")
	}

	if capturedReq.Name != "pawpantry" {
		t.Errorf("name = %q, want %q", capturedReq.Name, "pawpantry")
	}
	if capturedReq.GitSource == nil {
		t.Fatal("gitSource is nil")
	}
	if capturedReq.GitSource.Type != "github" {
		t.Errorf("gitSource.type = %q, want %q", capturedReq.GitSource.Type, "github")
	}
	if capturedReq.GitSource.Org != "withkivo" {
		t.Errorf("gitSource.org = %q, want %q", capturedReq.GitSource.Org, "withkivo")
	}
	if capturedReq.GitSource.Repo != "pawpantry" {
		t.Errorf("gitSource.repo = %q, want %q", capturedReq.GitSource.Repo, "pawpantry")
	}
	if capturedReq.GitSource.Ref != "main" {
		t.Errorf("gitSource.ref = %q, want %q", capturedReq.GitSource.Ref, "main")
	}
}

func TestDeployToVercel_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"forbidden","message":"Invalid token"}}`))
	}))
	defer srv.Close()

	d := New("gh-token", "bad-token", "withkivo")
	d.VercelBaseURL = srv.URL

	_, err := d.DeployToVercel(context.Background(), "venture")
	if err == nil {
		t.Fatal("expected error for 403 response, got nil")
	}
}

func TestDeployToVercel_EmptyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(vercelDeployResponse{
			ID:  "dpl_abc",
			URL: "",
		})
	}))
	defer srv.Close()

	d := New("gh-token", "vc-token", "withkivo")
	d.VercelBaseURL = srv.URL

	_, err := d.DeployToVercel(context.Background(), "venture")
	if err == nil {
		t.Fatal("expected error for empty URL, got nil")
	}
}

// --- New constructor test ---

func TestNew_DefaultBaseURLs(t *testing.T) {
	d := New("gh", "vc", "org")

	if d.GitHubBaseURL != defaultGitHubBaseURL {
		t.Errorf("GitHubBaseURL = %q, want %q", d.GitHubBaseURL, defaultGitHubBaseURL)
	}
	if d.VercelBaseURL != defaultVercelBaseURL {
		t.Errorf("VercelBaseURL = %q, want %q", d.VercelBaseURL, defaultVercelBaseURL)
	}
	if d.githubToken != "gh" {
		t.Errorf("githubToken = %q, want %q", d.githubToken, "gh")
	}
	if d.vercelToken != "vc" {
		t.Errorf("vercelToken = %q, want %q", d.vercelToken, "vc")
	}
	if d.githubOrg != "org" {
		t.Errorf("githubOrg = %q, want %q", d.githubOrg, "org")
	}
}
