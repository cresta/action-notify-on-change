package notificationloader

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/notificationfile"
	"github.com/google/go-github/v48/github"
	"gopkg.in/yaml.v2"
)

const notificationFile = ".action-notify-on-change.yaml"

type NotificationLoader struct {
}

func (n *NotificationLoader) LoadNotificationForPath(ctx context.Context, path string) (*notificationfile.NotificationFile, error) {
	filePath := filepath.Join(path, notificationFile)
	// Note: If we get throttled here, we can cache results
	fc, dc, res, err := client.Repositories.GetContents(ctx, ref.Owner, ref.Repo, filePath, &github.RepositoryContentGetOptions{Ref: ref.Sha})
	if err != nil {
		if res != nil && res.StatusCode == http.StatusNotFound {
			return &notificationfile.NotificationFile{}, nil
		}
		return nil, fmt.Errorf("failed to get contents for %s: %w", path, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return &notificationfile.NotificationFile{}, nil
	}
	if dc != nil {
		// A directory: ignore it
		return &notificationfile.NotificationFile{}, nil
	}
	if fc == nil {
		return nil, fmt.Errorf("failed to get contents for %s: no file contents", path)
	}
	fileContent, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get file content for %s: %w", filePath, err)
	}
	var ret notificationfile.NotificationFile
	if err := yaml.Unmarshal([]byte(fileContent), &ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file %s: %w", filePath, err)
	}
	return &ret, nil
}
