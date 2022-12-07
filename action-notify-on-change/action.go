package main

import (
	"context"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/actionlogic"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"
	"go.uber.org/fx"
)

// Action allows using fx to run CLI actions
// Basically from https://github.com/uber-go/fx/issues/755
type Action struct {
	sh     fx.Shutdowner
	logic  *actionlogic.ActionLogic
	logger logger.Logger
}

func newAction(lc fx.Lifecycle, sh fx.Shutdowner, logic *actionlogic.ActionLogic, logger logger.Logger) *Action {
	act := &Action{
		sh:     sh,
		logic:  logic,
		logger: logger,
	}
	lc.Append(fx.Hook{
		OnStart: act.start,
	})

	return act
}

func (a *Action) start(_ context.Context) error {
	go a.run()
	return nil
}

func (a *Action) run() {
	a.logger.Debugf("Starting action")
	defer a.logger.Debugf("Exiting action")
	runErr := a.logic.Run(context.Background())
	if runErr != nil {
		a.logger.Errorf("Failed to run action: %v", runErr)
	}
	if err := a.sh.Shutdown(); err != nil {
		a.logger.Errorf("Failed to shutdown: %v", err)
	}
}
