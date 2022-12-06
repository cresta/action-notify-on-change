package changecreator

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/annotatedinfo"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/changetosend"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile/notificationmerger"
	"golang.org/x/sync/errgroup"
)

type ChangeCreator struct {
	NotificationMerger *notificationmerger.NotificationMerger
	ghClient           *ghclient.GhClient
	cfg                *config.Config
	annotatedInfo      *annotatedinfo.AnnotatedInfo
}

func (c *ChangeCreator) CreateChanges(ctx context.Context, changedFiles []string) ([]changetosend.ChangeToSend, error) {
	// For each changed file, find the notification file
	// Merge them together
	// Create a changetosend.ChangeToSend for each notification
	// Return the list of changes
	type changeByIndex struct {
		change *changetosend.ChangeToSend
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
	ret := make([]changetosend.ChangeToSend, 0, len(changesByIndex))
	for _, changeByIndex := range changesByIndex {
		ret = append(ret, *changeByIndex.change)
	}
	return changetosend.MergeCommon(ret), nil
}

func (c *ChangeCreator) CreateChangesForFile(ctx context.Context, file string) (*changetosend.ChangeToSend, error) {
	notification, err := c.NotificationMerger.Merge(ctx, file)
	if err != nil {
		return nil, fmt.Errorf("failed to merge notifications for path %s: %w", file, err)
	}
	if notification == nil {
		return nil, nil
	}
	notifMsg, err := notification.ProcessTemplate()
	if err != nil {
		return nil, fmt.Errorf("failed to process template for notification %v: %w", notification, err)
	}

	change := changetosend.ChangeToSend{
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
		change.Users = notification.AllUsers(c.cfg.ChangeType)
		change.Channel = notification.Channel(c.cfg.ChangeType)
	case config.ChangeTypeCommit:
		change.Users = notification.AllUsers(c.cfg.ChangeType)
		change.Channel = notification.Channel(c.cfg.ChangeType)
	default:
		panic(fmt.Sprintf("unknown change type %d", c.cfg.ChangeType))
	}
	if change.Channel == "" {
		return nil, nil
	}
	return &change, nil
}
