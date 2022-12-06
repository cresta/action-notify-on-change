package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile"

	"github.com/sethvargo/go-githubactions"
)

func main() {
	l := Logic{
		Action: githubactions.New(),
	}
	if err := l.Run(context.Background()); err != nil {
		l.Action.Fatalf("Error: %v", err)
	}
}

type ActionStub interface {
	Fatalf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	GetInput(name string) string
	Context() (*githubactions.GitHubContext, error)
}

var _ ActionStub = &githubactions.Action{}

type Logic struct {
	Action ActionStub
}

func (l *Logic) Run(ctx context.Context) error {
	l.Action.Infof("Starting action-notify-on-change")
	// TODO: The graphql client is currently unused
	// ghGraphqlClient, err := NewGithubGraphQLClient(ctx, l.Action.GetInput("github-token"))
	// if err != nil {
	//   return fmt.Errorf("failed to create github client: %w", err)
	// }
	ghClient, err := NewGithubClient(ctx, l.Action.GetInput("github-token"))
	if err != nil {
		return fmt.Errorf("failed to create github client: %w", err)
	}
	l.Action.Infof("Created github client")
	ghCtx, err := l.Action.Context()
	if err != nil {
		return fmt.Errorf("failed to get github context: %w", err)
	}
	input, err := CalculateInput(ctx, ghCtx, ghClient)
	if err != nil {
		return fmt.Errorf("failed to calculate input: %w", err)
	}
	l.Action.Infof("Calculated input: %+v", input)
	changedFiles := removeEmptyAndDeDup(input.ChangedFiles)
	if len(changedFiles) == 0 {
		l.Action.Infof("No changed files, skipping")
		return nil
	}
	l.Action.Infof("Changed files: %s", strings.Join(changedFiles, ", "))
	slackClient, err := newSlackClient(l.Action.GetInput("slack-token"), l.Action)
	if err != nil {
		return fmt.Errorf("failed to create slack client: %w", err)
	}
	l.Action.Infof("Created slack client")
	changes, err := CreateChanges(ctx, changedFiles, notificationfile.ChangeTypePullRequest, input.PullRequestNumber, input.CommitSha, l.Action, input.RepoReference(), ghClient, input)
	if err != nil {
		return fmt.Errorf("failed to create changes: %w", err)
	}
	l.Action.Infof("Changes: %v", changes)
	if err := sendChanges(ctx, slackClient, changes); err != nil {
		return fmt.Errorf("failed to send changes: %w", err)
	}
	l.Action.Infof("Sent changes")
	return nil
}
