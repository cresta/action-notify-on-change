package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"

	"go.uber.org/fx"

	"github.com/stretchr/testify/require"
)

func TestLogic(t *testing.T) {
	var cfg config.Config
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
	fx.New(fx.WithLogger(logger.NewFxLogger), moduleMainSetup, fx.Supply(cfg, t), fx.Provide(fx.Annotate(logger.NewTestLogger, fx.As(new(logger.Logger))))).Run()
}
