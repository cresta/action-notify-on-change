package actionlogic

import (
	"context"
	"fmt"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/annotatedinfo"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/changetosend"
)

type ActionLogic struct {
	Fetcher annotatedinfo.Fetch
	Sender  changetosend.Sender
	Creator *changetosend.Creator
	logger  logger.Logger
}

func New(logger logger.Logger, fetcher annotatedinfo.Fetch, sender changetosend.Sender, creator *changetosend.Creator) *ActionLogic {
	return &ActionLogic{
		Fetcher: fetcher,
		Sender:  sender,
		Creator: creator,
		logger:  logger,
	}
}

func (a *ActionLogic) Run(ctx context.Context) error {
	a.logger.Infof("Fetching annotated info")
	annotatedInfo, err := a.Fetcher.Populate(ctx)
	if err != nil {
		return fmt.Errorf("failed to populate annotated info: %w", err)
	}
	a.logger.Infof("Creating changes")
	changes, err := a.Creator.CreateChanges(ctx, annotatedInfo.ChangedFiles)
	if err != nil {
		return fmt.Errorf("failed to create changes: %w", err)
	}
	a.logger.Infof("Sending messages")
	if err := changetosend.SendMessagesInParallel(ctx, a.Sender, changes); err != nil {
		return fmt.Errorf("failed to send messages in parallel: %w", err)
	}
	return nil
}
