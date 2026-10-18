package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/httpx"
)

// pullRequestBatchSize bounds how many pull requests one GraphQL request reads.
const pullRequestBatchSize = 50

// Owner and repository names are inlined into the GraphQL query, so they are only accepted in these shapes.
var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*)$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

var _ integrations.PullRequestStatusBatcher = (*Provider)(nil)

type pullRequestRef struct {
	owner  string
	repo   string
	number int
}

// parsePullRequestURL reads the owner, repository and number from a URL such as
// https://github.com/owner/repo/pull/7.
func parsePullRequestURL(prURL string) (pullRequestRef, error) {
	invalid := fmt.Errorf("invalid pull request URL: %q", prURL)
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host == "" {
		return pullRequestRef{}, invalid
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return pullRequestRef{}, invalid
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return pullRequestRef{}, invalid
	}
	if !ownerPattern.MatchString(parts[0]) || !repoPattern.MatchString(parts[1]) {
		return pullRequestRef{}, invalid
	}
	return pullRequestRef{owner: parts[0], repo: parts[1], number: number}, nil
}

// graphqlURL returns the GraphQL endpoint for the configured API root. GitHub Enterprise serves it under
// /api/graphql when the REST root ends in /api/v3.
func (p *Provider) graphqlURL() string {
	if strings.HasSuffix(p.baseURL, "/api/v3") {
		return strings.TrimSuffix(p.baseURL, "/api/v3") + "/api/graphql"
	}
	return p.baseURL + "/graphql"
}

type ghGraphQLPullRequest struct {
	Number   int     `json:"number"`
	State    string  `json:"state"`
	Merged   bool    `json:"merged"`
	MergedAt *string `json:"mergedAt"`
	URL      string  `json:"url"`
}

type ghPullRequestBatchResponse struct {
	Data map[string]*struct {
		PullRequest *ghGraphQLPullRequest `json:"pullRequest"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// GetPullRequestStatuses reads several pull requests with one GraphQL request per batch of up to 50, instead of
// one REST request each. Each pull request is addressed by the owner and repository in its own URL. A URL that
// cannot be parsed, or that cannot be read, gets its own error; a failed batch fails only its own URLs.
func (p *Provider) GetPullRequestStatuses(ctx context.Context, plainSecret string, prURLs []string) map[string]integrations.PullRequestStatusResult {
	results := make(map[string]integrations.PullRequestStatusResult, len(prURLs))
	var refs []pullRequestRef
	var keys []string
	seen := make(map[string]bool, len(prURLs))
	for _, prURL := range prURLs {
		if seen[prURL] {
			continue
		}
		seen[prURL] = true
		ref, err := parsePullRequestURL(prURL)
		if err != nil {
			results[prURL] = integrations.PullRequestStatusResult{Err: err}
			continue
		}
		refs = append(refs, ref)
		keys = append(keys, prURL)
	}

	for start := 0; start < len(refs); start += pullRequestBatchSize {
		end := min(start+pullRequestBatchSize, len(refs))
		p.readPullRequestBatch(ctx, plainSecret, refs[start:end], keys[start:end], results)
	}
	return results
}

// readPullRequestBatch reads one batch with a single GraphQL request and records each result under its URL.
func (p *Provider) readPullRequestBatch(ctx context.Context, token string, refs []pullRequestRef, keys []string, results map[string]integrations.PullRequestStatusResult) {
	var query strings.Builder
	query.WriteString("query {")
	for i, ref := range refs {
		fmt.Fprintf(&query, " pr%d: repository(owner: %q, name: %q) { pullRequest(number: %d) { number state merged mergedAt url } }",
			i, ref.owner, ref.repo, ref.number)
	}
	query.WriteString(" }")

	failBatch := func(err error) {
		for _, key := range keys {
			results[key] = integrations.PullRequestStatusResult{Err: err}
		}
	}

	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	body := map[string]string{"query": query.String()}
	resp, err := httpx.Do(ctx, p.httpClient, http.MethodPost, p.graphqlURL(), body, headers, "github graphql")
	if err != nil {
		failBatch(err)
		return
	}
	defer resp.Body.Close()

	if err := httpx.CheckSuccess(resp, "github graphql returned"); err != nil {
		failBatch(err)
		return
	}
	decoded, err := httpx.DecodeJSON[ghPullRequestBatchResponse](resp.Body, "pull request batch")
	if err != nil {
		failBatch(err)
		return
	}
	if decoded.Data == nil {
		msg := "empty response"
		if len(decoded.Errors) > 0 {
			msg = decoded.Errors[0].Message
		}
		failBatch(fmt.Errorf("github graphql: %s", msg))
		return
	}

	for i := range refs {
		key := keys[i]
		entry := decoded.Data[fmt.Sprintf("pr%d", i)]
		if entry == nil || entry.PullRequest == nil {
			results[key] = integrations.PullRequestStatusResult{
				Err: fmt.Errorf("pull request %s was not found or is not readable", key),
			}
			continue
		}
		results[key] = integrations.PullRequestStatusResult{Status: toPullRequestStatus(entry.PullRequest)}
	}
}

// toPullRequestStatus maps a GraphQL pull request onto the REST status shape: a merged pull request reports
// state "closed", as REST does.
func toPullRequestStatus(pr *ghGraphQLPullRequest) *integrations.PullRequestStatus {
	state := strings.ToLower(pr.State)
	if pr.Merged {
		state = "closed"
	}
	status := &integrations.PullRequestStatus{
		Number:  pr.Number,
		State:   state,
		Merged:  pr.Merged,
		HTMLURL: pr.URL,
	}
	if pr.MergedAt != nil {
		status.MergedAt = *pr.MergedAt
	}
	return status
}
