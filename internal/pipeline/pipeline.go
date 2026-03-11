package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hyagh/kivo/internal/deploy"
)

// Pipeline orchestrates the venture creation stages: research, build, deploy,
// and marketing. Each stage runs sequentially, updating the Venture as it
// progresses.
type Pipeline struct {
	apiKey   string
	model    string
	deployer *deploy.Deployer
}

// New creates a Pipeline that uses the given Anthropic API key and model for
// all Claude calls. The deployer handles GitHub repo creation and Vercel
// deployment; pass nil to fall back to stub behavior.
func New(apiKey, model string, deployer *deploy.Deployer) *Pipeline {
	return &Pipeline{
		apiKey:   apiKey,
		model:    model,
		deployer: deployer,
	}
}

// Run executes the full venture pipeline for the given product idea. It returns
// the completed Venture or an error if any stage fails.
func (p *Pipeline) Run(ctx context.Context, productName, ideaSummary string) (*Venture, error) {
	now := time.Now()
	v := &Venture{
		ID:          uuid.New().String(),
		ProductName: productName,
		IdeaSummary: ideaSummary,
		Stage:       StageResearch,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	log.Printf("pipeline: starting venture %s for %q", v.ID, productName)

	// Stage 1: Research
	log.Printf("pipeline: [%s] running research stage", v.ID)
	research, err := p.research(ctx, productName, ideaSummary)
	if err != nil {
		return p.fail(v, "research", err), fmt.Errorf("research stage: %w", err)
	}
	v.Research = research
	v.Stage = StageBuild
	v.UpdatedAt = time.Now()
	log.Printf("pipeline: [%s] research complete — tagline: %q", v.ID, research.Tagline)

	// Stage 2: Build
	log.Printf("pipeline: [%s] running build stage", v.ID)
	build, err := p.build(ctx, v)
	if err != nil {
		return p.fail(v, "build", err), fmt.Errorf("build stage: %w", err)
	}
	v.Build = build
	v.Stage = StageDeploy
	v.UpdatedAt = time.Now()
	log.Printf("pipeline: [%s] build complete — landing page generated (%d bytes)", v.ID, len(build.LandingHTML))

	// Stage 3: Deploy
	log.Printf("pipeline: [%s] running deploy stage", v.ID)
	deploy, err := p.deploy(ctx, v)
	if err != nil {
		return p.fail(v, "deploy", err), fmt.Errorf("deploy stage: %w", err)
	}
	v.Deploy = deploy
	v.Stage = StageMarketing
	v.UpdatedAt = time.Now()
	log.Printf("pipeline: [%s] deploy complete — site: %s", v.ID, deploy.SiteURL)

	// Stage 4: Marketing
	log.Printf("pipeline: [%s] running marketing stage", v.ID)
	marketing, err := p.marketing(ctx, v)
	if err != nil {
		return p.fail(v, "marketing", err), fmt.Errorf("marketing stage: %w", err)
	}
	v.Marketing = marketing
	v.Stage = StageComplete
	v.UpdatedAt = time.Now()
	log.Printf("pipeline: [%s] pipeline complete", v.ID)

	return v, nil
}

// fail marks the venture as failed with context about which stage errored.
func (p *Pipeline) fail(v *Venture, stage string, err error) *Venture {
	v.Stage = StageFailed
	v.Error = fmt.Sprintf("%s: %v", stage, err)
	v.UpdatedAt = time.Now()
	log.Printf("pipeline: [%s] failed at %s: %v", v.ID, stage, err)
	return v
}

// research calls Claude to perform market research on the product idea.
func (p *Pipeline) research(ctx context.Context, productName, ideaSummary string) (*ResearchResult, error) {
	systemPrompt := `You are an elite startup strategist and market researcher. Given a product idea, perform deep market research and return your findings as structured JSON.

You MUST respond with ONLY valid JSON matching this exact schema — no markdown, no commentary, no code fences:
{
  "target_audience": "description of the ideal customer segment",
  "value_proposition": "clear one-sentence value proposition",
  "competitors": ["competitor1", "competitor2", "competitor3"],
  "pricing_strategy": "recommended pricing approach with specific price points",
  "mvp_features": ["feature1", "feature2", "feature3"],
  "domain_suggestions": ["domain1.com", "domain2.com", "domain3.com"],
  "tagline": "catchy marketing tagline"
}`

	userMessage := fmt.Sprintf("Product name: %s\n\nIdea summary: %s\n\nPerform thorough market research for this product. Identify the target audience, craft a compelling value proposition, find 3-5 real or likely competitors, suggest a pricing strategy, list 5-7 MVP features, suggest 3-5 available domain names, and create a catchy tagline.", productName, ideaSummary)

	raw, err := callClaude(ctx, p.apiKey, p.model, systemPrompt, userMessage, 2048)
	if err != nil {
		return nil, fmt.Errorf("calling claude: %w", err)
	}

	cleaned := cleanJSON(raw)

	var result ResearchResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing research response: %w (raw: %.500s)", err, raw)
	}

	return &result, nil
}

// build calls Claude to generate a complete landing page for the venture.
func (p *Pipeline) build(ctx context.Context, v *Venture) (*BuildResult, error) {
	if v.Research == nil {
		return nil, fmt.Errorf("research result is required before build stage")
	}

	systemPrompt := `You are an expert web designer and frontend developer. Generate a complete, production-ready landing page as a single HTML file using Tailwind CSS via CDN.

The page must include:
- Hero section with the product tagline, a subtitle explaining the value proposition, and a prominent CTA button
- Features section showcasing the MVP features with icons or visual elements
- Social proof / trust section
- Pricing placeholder section
- Final CTA section
- Footer with copyright

Design requirements:
- Modern, clean, professional design
- Responsive (mobile-first)
- Use Tailwind CSS CDN (https://cdn.tailwindcss.com)
- Smooth scroll behavior
- Pleasant color scheme that matches the product's vibe

You MUST respond with ONLY valid JSON matching this exact schema — no markdown, no commentary, no code fences:
{
  "repo_url": "",
  "landing_html": "<the complete HTML file as a string>",
  "features": "comma-separated list of features showcased on the page"
}`

	features := strings.Join(v.Research.MVPFeatures, ", ")
	userMessage := fmt.Sprintf(`Product: %s
Tagline: %s
Value Proposition: %s
Target Audience: %s
MVP Features: %s
Pricing Strategy: %s

Generate a stunning landing page for this product.`, v.ProductName, v.Research.Tagline, v.Research.ValueProposition, v.Research.TargetAudience, features, v.Research.PricingStrategy)

	raw, err := callClaude(ctx, p.apiKey, p.model, systemPrompt, userMessage, 16384)
	if err != nil {
		return nil, fmt.Errorf("calling claude: %w", err)
	}

	cleaned := cleanJSON(raw)

	var result BuildResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing build response: %w (raw: %.500s)", err, raw)
	}

	return &result, nil
}

// deploy creates a GitHub repo, pushes the landing page, and triggers a Vercel
// deployment. If no deployer is configured it falls back to stub results.
func (p *Pipeline) deploy(ctx context.Context, v *Venture) (*DeployResult, error) {
	if p.deployer == nil {
		log.Println("deploy stage: no deployer configured, returning stubs")
		return &DeployResult{
			SiteURL:  "pending",
			Domain:   "pending",
			DeployID: "pending",
		}, nil
	}

	if v.Build == nil || v.Build.LandingHTML == "" {
		return nil, fmt.Errorf("build result with landing HTML is required before deploy stage")
	}

	// Derive a repo name from the product name: lowercase, hyphens, no spaces.
	repoName := sanitizeRepoName(v.ProductName)

	// Step 1: Create the GitHub repo.
	repoURL, err := p.deployer.CreateRepo(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("creating github repo: %w", err)
	}
	log.Printf("deploy: created repo %s", repoURL)

	// Update the build result with the real repo URL.
	v.Build.RepoURL = repoURL

	// Step 2: Push the landing page as index.html.
	files := map[string]string{
		"index.html": v.Build.LandingHTML,
	}
	if err := p.deployer.PushFiles(ctx, repoName, files); err != nil {
		return nil, fmt.Errorf("pushing files to github: %w", err)
	}
	log.Printf("deploy: pushed %d file(s) to %s", len(files), repoName)

	// Step 3: Trigger a Vercel deployment from the repo.
	siteURL, err := p.deployer.DeployToVercel(ctx, repoName)
	if err != nil {
		return nil, fmt.Errorf("deploying to vercel: %w", err)
	}
	log.Printf("deploy: vercel site live at %s", siteURL)

	return &DeployResult{
		SiteURL:  siteURL,
		Domain:   repoName + ".vercel.app",
		DeployID: repoName,
	}, nil
}

// sanitizeRepoName converts a product name into a valid GitHub repo name.
// It lowercases, replaces spaces/underscores with hyphens, and strips anything
// that is not alphanumeric or a hyphen.
func sanitizeRepoName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")

	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}

	// Trim leading/trailing hyphens and collapse multiple hyphens.
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")

	if result == "" {
		result = "venture"
	}
	return result
}

// marketing calls Claude to generate a full marketing content package for the
// venture, including social posts, a launch email, SEO meta tags, a press
// release blurb, and blog post title ideas.
func (p *Pipeline) marketing(ctx context.Context, v *Venture) (*MarketingResult, error) {
	if v.Research == nil {
		return nil, fmt.Errorf("research result is required before marketing stage")
	}

	systemPrompt := `You are an expert startup marketing strategist and copywriter. Given a product's research data, generate a complete marketing content package as structured JSON.

You MUST respond with ONLY valid JSON matching this exact schema — no markdown, no commentary, no code fences:
{
  "social_posts": [
    "tweet 1 (under 280 characters)",
    "tweet 2 (under 280 characters)",
    "tweet 3 (under 280 characters)",
    "tweet 4 (under 280 characters)",
    "tweet 5 (under 280 characters)"
  ],
  "launch_email": {
    "subject": "compelling email subject line",
    "body": "full HTML email body for the product launch announcement"
  },
  "seo": {
    "title": "SEO-optimized page title (50-60 chars)",
    "description": "meta description for search engines (150-160 chars)",
    "keywords": ["keyword1", "keyword2", "keyword3", "keyword4", "keyword5"]
  },
  "press_blurb": "one paragraph press release blurb announcing the product",
  "blog_titles": [
    "blog post title idea 1",
    "blog post title idea 2",
    "blog post title idea 3"
  ]
}

Rules:
- Each social post MUST be under 280 characters (Twitter/X format). Include relevant hashtags.
- The launch email body should be professional, engaging, and include a clear call-to-action.
- SEO title should be 50-60 characters, description 150-160 characters.
- The press blurb should be exactly one paragraph, suitable for a press release.
- Blog titles should be SEO-friendly and address pain points of the target audience.
- All content should be compelling, specific, and ready to publish.`

	features := strings.Join(v.Research.MVPFeatures, ", ")
	userMessage := fmt.Sprintf(`Product: %s
Tagline: %s
Value Proposition: %s
Target Audience: %s
Key Features: %s
Pricing Strategy: %s
Idea Summary: %s

Generate a complete marketing content package for this product launch.`,
		v.ProductName,
		v.Research.Tagline,
		v.Research.ValueProposition,
		v.Research.TargetAudience,
		features,
		v.Research.PricingStrategy,
		v.IdeaSummary,
	)

	raw, err := callClaude(ctx, p.apiKey, p.model, systemPrompt, userMessage, 4096)
	if err != nil {
		return nil, fmt.Errorf("calling claude: %w", err)
	}

	cleaned := cleanJSON(raw)

	var result MarketingResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parsing marketing response: %w (raw: %.500s)", err, raw)
	}

	return &result, nil
}

// cleanJSON strips markdown code fences and leading/trailing whitespace that
// Claude sometimes wraps around JSON responses.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	// Remove ```json ... ``` wrapping
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
