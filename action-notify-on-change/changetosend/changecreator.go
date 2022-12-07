package changetosend

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/annotatedinfo"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/notification"
	"golang.org/x/sync/errgroup"
)

type Creator struct {
	NotificationMerger   *notification.Merger
	ghClient             *ghclient.GhClient
	cfg                  config.Config
	annotatedInfoFetcher annotatedinfo.Fetch
	logger               logger.Logger
	annotatedInfo        *annotatedinfo.AnnotatedInfo
}

func NewCreator(cfg config.Config, ghClient *ghclient.GhClient, annotatedInfo annotatedinfo.Fetch, notificationMerger *notification.Merger, logger logger.Logger) *Creator {
	return &Creator{
		NotificationMerger:   notificationMerger,
		ghClient:             ghClient,
		cfg:                  cfg,
		logger:               logger,
		annotatedInfoFetcher: annotatedInfo,
	}
}

func (c *Creator) CreateChanges(ctx context.Context, changedFiles []string) ([]ChangeToSend, error) {
	ai, err := c.annotatedInfoFetcher.Populate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to populate annotated info: %w", err)
	}
	c.annotatedInfo = ai
	// For each changed file, find the notification file
	// Merge them together
	// Create a changetosend.ChangeToSend for each notification
	// Return the list of changes
	type changeByIndex struct {
		change *ChangeToSend
		index  int
	}
	changesByIndex := make([]changeByIndex, 0, len(changedFiles))
	changesByIndexMu := sync.Mutex{}
	eg, egCtx := errgroup.WithContext(ctx)
	for idx, file := range changedFiles {
		idx := idx
		file := file
		eg.Go(func() error {
			change, err := c.CreateChangesForFile(egCtx, file)
			if err != nil {
				return fmt.Errorf("failed to create change for file %s: %w", file, err)
			}
			if change == nil {
				return nil
			}
			changesByIndexMu.Lock()
			defer changesByIndexMu.Unlock()
			changesByIndex = append(changesByIndex, changeByIndex{
				change: change,
				index:  idx,
			})
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to create changes: %w", err)
	}
	sort.Slice(changesByIndex, func(i, j int) bool {
		return changesByIndex[i].index < changesByIndex[j].index
	})
	ret := make([]ChangeToSend, 0, len(changesByIndex))
	for _, changeByIndex := range changesByIndex {
		ret = append(ret, *changeByIndex.change)
	}
	return MergeCommon(ret), nil
}

func (c *Creator) CreateChangesForFile(ctx context.Context, file string) (*ChangeToSend, error) {
	notif, err := c.NotificationMerger.Merge(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("failed to merge notifications for path %s: %w", file, err)
	}
	if notif == nil {
		return nil, nil
	}
	notifMsg, err := notif.ProcessTemplate(c.cfg.ChangeType)
	if err != nil {
		return nil, fmt.Errorf("failed to process template for notification %v: %w", notif, err)
	}
	if notifMsg == "" {
		c.logger.Debugf("notification message is empty for %s", file)
	}
	change := ChangeToSend{
		ModifiedFiles: []string{file},
		Message:       notifMsg,
		CommitSha:     c.cfg.CommitSha,
		Creator:       c.annotatedInfo.PrCreator,
		Branch:        c.annotatedInfo.PrBase,
		LinkToChange:  c.annotatedInfo.LinkToChange,
		LinkToAuthor:  c.annotatedInfo.LinkToAuthor,
	}
	switch c.cfg.ChangeType {
	case config.ChangeTypePullRequest:
		change.PullRequestNumber = c.cfg.PullRequestNumber
		change.Users = notif.AllUsers(c.cfg.ChangeType)
		change.Channel = notif.Channel(c.cfg.ChangeType)
	case config.ChangeTypeCommit:
		change.Users = notif.AllUsers(c.cfg.ChangeType)
		change.Channel = notif.Channel(c.cfg.ChangeType)
	default:
		panic(fmt.Sprintf("unknown change type %d", c.cfg.ChangeType))
	}
	if change.Channel == "" {
		return nil, nil
	}
	return &change, nil
}
