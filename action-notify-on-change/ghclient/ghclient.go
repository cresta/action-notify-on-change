package ghclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
	"github.com/google/go-github/v48/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

type GhClient struct {
	restClient    *github.Client
	graphqlClient *githubv4.Client
	cfg           config.Config
	logger        logger.Logger
}

func New(cfg config.Config, logger logger.Logger) (*GhClient, error) {
	// TODO: What is the right way to do this?
	ctx := context.Background()
	restClient, err := newGithubClient(ctx, cfg.GithubToken, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create github rest client: %w", err)
	}
	graphqlClient, err := newGithubGraphQLClient(ctx, cfg.GithubToken, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create github graphql client: %w", err)
	}
	return &GhClient{
		restClient:    restClient,
		graphqlClient: graphqlClient,
		cfg:           cfg,
		logger:        logger,
	}, nil
}

func newGithubClient(ctx context.Context, token string, l logger.Logger) (*github.Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	s, _, err := client.Zen(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query github zen: %w", err)
	}
	l.Infof("github zen: %s", s)
	return client, nil
}

func newGithubGraphQLClient(ctx context.Context, token string, l logger.Logger) (*githubv4.Client, error) {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(ctx, src)

	client := githubv4.NewClient(httpClient)
	// Test query to make sure the token works
	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	err := client.Query(ctx, &query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query github viewer: %w", err)
	}
	l.Infof("github viewer: %s", query.Viewer.Login)
	return client, nil
}

func (g *GhClient) GetContents(ctx context.Context, filePath string) ([]byte, error) {
	g.logger.Debugf("getting contents of %s", filePath)
	fc, dc, res, err := g.restClient.Repositories.GetContents(ctx, g.cfg.RepoOwner, g.cfg.RepoName, filePath, &github.RepositoryContentGetOptions{Ref: g.cfg.CommitSha})
	if err != nil {
		if res != nil && res.StatusCode == http.StatusNotFound {
			g.logger.Debugf("file %s not found", filePath)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get contents of %s: %w", filePath, err)
	}
	if res.StatusCode == http.StatusNotFound {
		g.logger.Debugf("file %s not found", filePath)
		return nil, nil
	}
	if dc != nil {
		// A directory: ignore it
		return nil, nil
	}
	if fc == nil {
		return nil, fmt.Errorf("failed to get contents for %s: no file contents", filePath)
	}
	fileContent, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get contents for %s: %w", filePath, err)
	}
	return []byte(fileContent), nil
}

type PrInfo struct {
	PrLink       string
	AuthorLink   string
	PrCreator    string
	PrBase       string
	ChangedFiles []string
}

func (g *GhClient) PrInfo(ctx context.Context) (*PrInfo, error) {
	g.logger.Debugf("getting pr info for %s", g.cfg.CommitSha)
	ret := &PrInfo{}
	var opts github.ListOptions
	for {
		prInfo, resp, err := g.restClient.PullRequests.Get(ctx, g.cfg.RepoOwner, g.cfg.RepoName, g.cfg.PullRequestNumber)
		if err != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get pull request info for PR %d: %w", g.cfg.PullRequestNumber, err)
		}
		if ret.PrLink == "" {
			ret.PrLink = prInfo.GetHTMLURL()
		}
		if ret.AuthorLink == "" {
			ret.AuthorLink = prInfo.User.GetHTMLURL()
		}
		if ret.PrCreator == "" {
			ret.PrCreator = prInfo.User.GetLogin()
		}
		if ret.PrBase == "" {
			ret.PrBase = prInfo.GetBase().GetRef()
		}
		files, resp, err := g.restClient.PullRequests.ListFiles(ctx, g.cfg.RepoOwner, g.cfg.RepoName, g.cfg.PullRequestNumber, &opts)
		if err != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to list pull request files: %s", resp.Status)
		}
		for _, file := range files {
			ret.ChangedFiles = append(ret.ChangedFiles, file.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return ret, nil
}

type CommitInfo struct {
	AuthorLink   string
	LinkToChange string
	AuthorName   string
	ChangedFiles []string
}

func (g *GhClient) GetCommit(ctx context.Context) (*CommitInfo, error) {
	g.logger.Debugf("getting commit info for %s", g.cfg.CommitSha)
	var opts github.ListOptions
	ret := &CommitInfo{}
	for {
		commit, resp, err := g.restClient.Repositories.GetCommit(ctx, g.cfg.RepoOwner, g.cfg.RepoName, g.cfg.CommitSha, &opts)
		if err != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get commit info for commit %s: %w", g.cfg.CommitSha, err)
		}
		if ret.AuthorName == "" {
			ret.AuthorName = commit.GetAuthor().GetLogin()
		}
		if ret.LinkToChange == "" {
			ret.LinkToChange = commit.GetHTMLURL()
		}
		if ret.AuthorLink == "" {
			ret.AuthorLink = commit.GetAuthor().GetHTMLURL()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to get commit: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to get commit: %s", resp.Status)
		}
		for _, file := range commit.Files {
			ret.ChangedFiles = append(ret.ChangedFiles, file.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return ret, nil
}
