package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setBaseURL overrides the package-level baseURL for testing.
func setBaseURL(url string) {
	baseURL = url
}

// newTestClient creates a Client pointed at the given test server URL.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c := NewClient("test-api-key", "ideas-db-id", "ventures-db-id")
	c.httpClient = &http.Client{}
	return c
}

// --- SaveIdea tests ---

func TestSaveIdea_Success(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/pages" {
			t.Errorf("expected /pages, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", got)
		}
		if got := r.Header.Get("Notion-Version"); got != notionVersion {
			t.Errorf("expected Notion-Version %s, got %s", notionVersion, got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", got)
		}

		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "page-123"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	// Override baseURL by swapping it — we need to point createPage at the test server.
	// Since baseURL is a package constant, we'll override the httpClient transport instead.
	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	idea := IdeaRecord{
		Subject:      "AI-powered gardening",
		Score:        85,
		MarketSize:   "large",
		Feasibility:  "moderate",
		ProductName:  "GreenBot",
		Summary:      "An AI that helps you garden",
		ShouldPursue: true,
		EmailID:      "email-456",
	}

	err := c.SaveIdea(context.Background(), idea)
	if err != nil {
		t.Fatalf("SaveIdea returned error: %v", err)
	}

	// Verify parent database ID
	parent, ok := received["parent"].(map[string]any)
	if !ok {
		t.Fatal("missing parent in request body")
	}
	if parent["database_id"] != "ideas-db-id" {
		t.Errorf("expected database_id ideas-db-id, got %v", parent["database_id"])
	}

	// Verify properties
	props, ok := received["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties in request body")
	}

	assertTitleProperty(t, props, "Idea", "AI-powered gardening")
	assertNumberProperty(t, props, "Score", 85)
	assertSelectProperty(t, props, "Market Size", "large")
	assertSelectProperty(t, props, "Feasibility", "moderate")
	assertRichTextProperty(t, props, "Product Name", "GreenBot")
	assertRichTextProperty(t, props, "Summary", "An AI that helps you garden")
	assertCheckboxProperty(t, props, "Should Pursue", true)
	assertRichTextProperty(t, props, "Email ID", "email-456")
}

func TestSaveIdea_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "validation_error",
			"message": "property Title is not supported",
		})
	}))
	defer srv.Close()

	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	c := newTestClient(t, srv.URL)
	err := c.SaveIdea(context.Background(), IdeaRecord{Subject: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}
}

// --- SaveVenture tests ---

func TestSaveVenture_Success(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "venture-page-789"})
	}))
	defer srv.Close()

	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	c := newTestClient(t, srv.URL)

	venture := VentureRecord{
		ProductName:      "GreenBot",
		Stage:            "research",
		SiteURL:          "https://greenbot.ai",
		RepoURL:          "https://github.com/hyagh/greenbot",
		Tagline:          "AI gardening made easy",
		TargetAudience:   "Home gardeners",
		ValueProposition: "Never kill another plant",
		Score:            85,
	}

	pageID, err := c.SaveVenture(context.Background(), venture)
	if err != nil {
		t.Fatalf("SaveVenture returned error: %v", err)
	}
	if pageID != "venture-page-789" {
		t.Errorf("expected page ID venture-page-789, got %s", pageID)
	}

	parent, ok := received["parent"].(map[string]any)
	if !ok {
		t.Fatal("missing parent")
	}
	if parent["database_id"] != "ventures-db-id" {
		t.Errorf("expected ventures-db-id, got %v", parent["database_id"])
	}

	props := received["properties"].(map[string]any)
	assertTitleProperty(t, props, "Product Name", "GreenBot")
	assertSelectProperty(t, props, "Stage", "research")
	assertURLProperty(t, props, "Site URL", "https://greenbot.ai")
	assertURLProperty(t, props, "Repo URL", "https://github.com/hyagh/greenbot")
	assertRichTextProperty(t, props, "Tagline", "AI gardening made easy")
	assertRichTextProperty(t, props, "Target Audience", "Home gardeners")
	assertRichTextProperty(t, props, "Value Proposition", "Never kill another plant")
	assertNumberProperty(t, props, "Score", 85)
}

func TestSaveVenture_OmitsEmptyURLs(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "v-1"})
	}))
	defer srv.Close()

	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	c := newTestClient(t, srv.URL)
	_, err := c.SaveVenture(context.Background(), VentureRecord{
		ProductName: "TestBot",
		Stage:       "research",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	props := received["properties"].(map[string]any)
	if _, ok := props["Site URL"]; ok {
		t.Error("expected Site URL to be omitted when empty")
	}
	if _, ok := props["Repo URL"]; ok {
		t.Error("expected Repo URL to be omitted when empty")
	}
}

// --- UpdateVenture tests ---

func TestUpdateVenture_Success(t *testing.T) {
	var (
		received   map[string]any
		gotMethod  string
		gotPath    string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "venture-page-789"})
	}))
	defer srv.Close()

	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	c := newTestClient(t, srv.URL)

	stage := "build"
	siteURL := "https://greenbot.ai/v2"

	err := c.UpdateVenture(context.Background(), "venture-page-789", VentureUpdate{
		Stage:   &stage,
		SiteURL: &siteURL,
	})
	if err != nil {
		t.Fatalf("UpdateVenture returned error: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", gotMethod)
	}
	if gotPath != "/pages/venture-page-789" {
		t.Errorf("expected /pages/venture-page-789, got %s", gotPath)
	}

	props := received["properties"].(map[string]any)
	assertSelectProperty(t, props, "Stage", "build")
	assertURLProperty(t, props, "Site URL", "https://greenbot.ai/v2")

	// RepoURL was nil, should not be present.
	if _, ok := props["Repo URL"]; ok {
		t.Error("expected Repo URL to be omitted when nil")
	}
}

func TestUpdateVenture_EmptyWhenNoFields(t *testing.T) {
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "v-1"})
	}))
	defer srv.Close()

	origBaseURL := baseURL
	defer func() { setBaseURL(origBaseURL) }()
	setBaseURL(srv.URL)

	c := newTestClient(t, srv.URL)
	err := c.UpdateVenture(context.Background(), "v-1", VentureUpdate{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	props := received["properties"].(map[string]any)
	if len(props) != 0 {
		t.Errorf("expected 0 properties, got %d", len(props))
	}
}

// --- assertion helpers ---

func assertTitleProperty(t *testing.T, props map[string]any, key, expected string) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	titleArr, ok := prop["title"].([]any)
	if !ok || len(titleArr) == 0 {
		t.Errorf("property %s has no title array", key)
		return
	}
	textObj := titleArr[0].(map[string]any)["text"].(map[string]any)
	if textObj["content"] != expected {
		t.Errorf("property %s: expected %q, got %q", key, expected, textObj["content"])
	}
}

func assertRichTextProperty(t *testing.T, props map[string]any, key, expected string) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	rtArr, ok := prop["rich_text"].([]any)
	if !ok || len(rtArr) == 0 {
		t.Errorf("property %s has no rich_text array", key)
		return
	}
	textObj := rtArr[0].(map[string]any)["text"].(map[string]any)
	if textObj["content"] != expected {
		t.Errorf("property %s: expected %q, got %q", key, expected, textObj["content"])
	}
}

func assertNumberProperty(t *testing.T, props map[string]any, key string, expected float64) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	got, ok := prop["number"].(float64)
	if !ok {
		t.Errorf("property %s: number is not float64", key)
		return
	}
	if got != expected {
		t.Errorf("property %s: expected %v, got %v", key, expected, got)
	}
}

func assertSelectProperty(t *testing.T, props map[string]any, key, expected string) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	sel, ok := prop["select"].(map[string]any)
	if !ok {
		t.Errorf("property %s has no select object", key)
		return
	}
	if sel["name"] != expected {
		t.Errorf("property %s: expected select %q, got %q", key, expected, sel["name"])
	}
}

func assertCheckboxProperty(t *testing.T, props map[string]any, key string, expected bool) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	got, ok := prop["checkbox"].(bool)
	if !ok {
		t.Errorf("property %s: checkbox is not bool", key)
		return
	}
	if got != expected {
		t.Errorf("property %s: expected %v, got %v", key, expected, got)
	}
}

func assertURLProperty(t *testing.T, props map[string]any, key, expected string) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	got, ok := prop["url"].(string)
	if !ok {
		t.Errorf("property %s: url is not string", key)
		return
	}
	if got != expected {
		t.Errorf("property %s: expected %q, got %q", key, expected, got)
	}
}

func assertHasDateProperty(t *testing.T, props map[string]any, key string) {
	t.Helper()
	prop, ok := props[key].(map[string]any)
	if !ok {
		t.Errorf("missing property %s", key)
		return
	}
	dateObj, ok := prop["date"].(map[string]any)
	if !ok {
		t.Errorf("property %s has no date object", key)
		return
	}
	start, ok := dateObj["start"].(string)
	if !ok || start == "" {
		t.Errorf("property %s: date.start is missing or empty", key)
	}
}
