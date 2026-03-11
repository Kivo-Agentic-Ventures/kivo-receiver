package pipeline

import "time"

// Stage represents the current phase of a venture in the pipeline.
type Stage string

const (
	StageResearch  Stage = "research"
	StageBuild     Stage = "build"
	StageDeploy    Stage = "deploy"
	StageMarketing Stage = "marketing"
	StageComplete  Stage = "complete"
	StageFailed    Stage = "failed"
)

// Venture tracks a product idea as it moves through the pipeline stages.
type Venture struct {
	ID          string           `json:"id"`
	ProductName string           `json:"product_name"`
	IdeaSummary string           `json:"idea_summary"`
	Stage       Stage            `json:"stage"`
	Research    *ResearchResult  `json:"research,omitempty"`
	Build       *BuildResult     `json:"build,omitempty"`
	Deploy      *DeployResult    `json:"deploy,omitempty"`
	Marketing   *MarketingResult `json:"marketing,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Error       string           `json:"error,omitempty"`
}

// ResearchResult holds market research output from the research stage.
type ResearchResult struct {
	TargetAudience    string   `json:"target_audience"`
	ValueProposition  string   `json:"value_proposition"`
	Competitors       []string `json:"competitors"`
	PricingStrategy   string   `json:"pricing_strategy"`
	MVPFeatures       []string `json:"mvp_features"`
	DomainSuggestions []string `json:"domain_suggestions"`
	Tagline           string   `json:"tagline"`
}

// BuildResult holds the generated landing page and related assets.
type BuildResult struct {
	RepoURL     string `json:"repo_url"`
	LandingHTML string `json:"landing_html"`
	Features    string `json:"features"`
}

// DeployResult holds deployment information for the venture's site.
type DeployResult struct {
	SiteURL  string `json:"site_url"`
	Domain   string `json:"domain"`
	DeployID string `json:"deploy_id"`
}

// MarketingResult holds generated marketing copy for the launch.
type MarketingResult struct {
	SocialPosts []string    `json:"social_posts"`
	LaunchEmail LaunchEmail `json:"launch_email"`
	SEO         SEOTags     `json:"seo"`
	PressBlurb  string      `json:"press_blurb"`
	BlogTitles  []string    `json:"blog_titles"`
}

// LaunchEmail holds a product launch email with subject and body.
type LaunchEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// SEOTags holds meta tags for search engine optimization.
type SEOTags struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
}
