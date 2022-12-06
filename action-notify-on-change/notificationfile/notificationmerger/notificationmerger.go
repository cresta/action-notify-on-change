package notificationmerger

import (
	"context"
	"fmt"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile/notificationloader"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile"
	"golang.org/x/sync/errgroup"
)

type NotificationMerger struct {
	NotificationLoader *notificationloader.NotificationLoader
}

func containsStopFile(path string) bool {
	// Check if the path contains a .notify-stop file
	// If it does, return true
	// Otherwise, return false
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func (n *NotificationMerger) Merge(ctx context.Context, path string) (*notificationfile.NotificationFile, error) {
	// Walk up the path, looking for notification files
	// Merge them together
	// Return the merged notification file
	path = filepath.Clean(path)
	rootPath := path
	type loadRetVal struct {
		idx          int
		notification *notificationfile.NotificationFile
	}
	var i int
	eg, egCtx := errgroup.WithContext(ctx)
	allRetValues := make([]loadRetVal, 0, 10)
	var allRetValuesMu sync.Mutex
	for i = 0; ; i++ {
		idx := i
		loadPath := path
		eg.Go(func() error {
			notification, err := n.NotificationLoader.LoadNotificationForPath(egCtx, loadPath)
			if err != nil {
				return fmt.Errorf("failed to load notification for path %s: %w", loadPath, err)
			}
			if notification != nil {
				notification.ChangedFile = rootPath
			}
			allRetValuesMu.Lock()
			defer allRetValuesMu.Unlock()
			allRetValues = append(allRetValues, loadRetVal{
				idx:          idx,
				notification: notification,
			})
			return nil
		})
		if containsStopFile(path) {
			break
		}
		newPath := filepath.Dir(path)

		if newPath == path {
			// We've reached the root
			break
		}
		path = newPath
	}
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to load notifications: %w", err)
	}
	sort.Slice(allRetValues, func(i, j int) bool {
		return allRetValues[i].idx < allRetValues[j].idx
	})
	ret := allRetValues[0].notification
	for idx := 1; idx < len(allRetValues); idx++ {
		allRetValues[idx-1].notification.Parent = allRetValues[idx].notification
	}
	return ret, nil
}
