package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sethvargo/go-githubactions"
)

type testingStub struct {
	t      *testing.T
	ctx    *githubactions.GitHubContext
	inputs map[string]string
}

func (t *testingStub) Fatalf(format string, args ...interface{}) {
	t.t.Fatalf(format, args...)
}

func (t *testingStub) Infof(format string, args ...interface{}) {
	t.t.Logf(format, args...)
}

func (t *testingStub) GetInput(name string) string {
	return t.inputs[name]
}

func (t *testingStub) Context() (*githubactions.GitHubContext, error) {
	return t.ctx, nil
}

var _ ActionStub = &testingStub{}

type IntegrationTestConfig struct {
	GithubToken string
	SlackToken  string
	Ctx         *githubactions.GitHubContext
}

func TestLogic(t *testing.T) {
	var cfg IntegrationTestConfig
	b, err := os.ReadFile("integration-test.json")
	if err != nil && os.IsNotExist(err) {
		t.Skip("skipping integration test: integration-test.json not found")
	}
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &cfg))
	ghToken := cfg.GithubToken
	if ghToken == "" {
		t.Skip("GITHUB_TOKEN not set")
	}
	slackToken := cfg.SlackToken
	if slackToken == "" {
		t.Skip("SLACK_TOKEN not set")
	}
	stub := &testingStub{
		t:   t,
		ctx: cfg.Ctx,
		inputs: map[string]string{
			"slack-token":  slackToken,
			"github-token": ghToken,
		},
	}
	l := Logic{
		Action: stub,
	}
	ctx := context.Background()
	if err := l.Run(ctx); err != nil {
		t.Fatal(err)
	}
}
