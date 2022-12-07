package config

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/sethvargo/go-githubactions"
)

func NewFromGithubActions(action *githubactions.Action) (Config, error) {
	ghCtx, err := action.Context()
	if err != nil {
		return Config{}, err
	}
	ghOwner, ghName := ghCtx.Repo()
	prNumber := 0
	ct := ChangeTypeCommit
	if ghCtx.EventName == "pull_request" {
		rgx := regexp.MustCompile(`^refs/pull/([0-9]+)/merge$`)
		matches := rgx.FindStringSubmatch(ghCtx.Ref)
		if len(matches) != 2 {
			return Config{}, fmt.Errorf("failed to parse pull request number from ref %s", ghCtx.Ref)
		}
		var err error
		prNumber, err = strconv.Atoi(matches[1])
		if err != nil {
			return Config{}, fmt.Errorf("failed to parse pull request number from ref %s: %w", ghCtx.Ref, err)
		}
		ct = ChangeTypePullRequest
	}
	return Config{
		GithubToken:       action.GetInput("github-token"),
		SlackToken:        action.GetInput("slack-token"),
		CommitSha:         ghCtx.SHA,
		RepoOwner:         ghOwner,
		RepoName:          ghName,
		BaseBranch:        ghCtx.BaseRef,
		Ref:               ghCtx.Ref,
		EventName:         ghCtx.EventName,
		RefName:           ghCtx.RefName,
		PullRequestNumber: prNumber,
		ChangeType:        ct,
	}, nil
}

func NewGithubActionsFromEnv() *githubactions.Action {
	return githubactions.New()
}
