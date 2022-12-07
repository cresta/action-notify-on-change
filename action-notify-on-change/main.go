package main

import (
	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/annotatedinfo"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/notification"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/changetosend"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/actionlogic"
	"go.uber.org/fx"
)

var moduleRunningInGithubActions = fx.Module("run-with-github", fx.Options(
	fx.Provide(
		config.NewGithubActionsFromEnv,
		config.NewFromGithubActions,
		fx.Annotate(logger.NewGhLogger, fx.As(new(logger.Logger))),
	)))

var moduleMainSetup = fx.Module("main-setup", fx.Options(
	fx.Provide(
		newAction,
		actionlogic.New,
		ghclient.New,
		fx.Annotate(changetosend.NewSlackDestination, fx.As(new(changetosend.Sender))),
		fx.Annotate(annotatedinfo.NewFromGh, fx.As(new(annotatedinfo.Fetch))),
		changetosend.NewCreator,
		notification.NewMerger,
		notification.NewLoader,
	),
	fx.Invoke(func(*Action) {}),
))

func main() {
	fx.New(fx.WithLogger(logger.NewFxLogger), moduleMainSetup, moduleRunningInGithubActions).Run()
}
