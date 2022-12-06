package notificationloader

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile"
	"gopkg.in/yaml.v2"
)

const notificationFile = ".action-notify-on-change.yaml"

type NotificationLoader struct {
	ghClient *ghclient.GhClient
}

func (n *NotificationLoader) LoadNotificationForPath(ctx context.Context, path string) (*notificationfile.NotificationFile, error) {
	filePath := filepath.Join(path, notificationFile)
	// Note: If we get throttled here, we can cache results
	fileContent, err := n.ghClient.GetContents(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get contents for %s: %w", path, err)
	}
	var ret notificationfile.NotificationFile
	if err := yaml.Unmarshal(fileContent, &ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file %s as yaml: %w", filePath, err)
	}
	return &ret, nil
}
