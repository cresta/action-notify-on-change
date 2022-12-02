package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sethvargo/go-githubactions"
	"github.com/slack-go/slack"
	"sigs.k8s.io/yaml"
)

type NotificationFile struct {
	PullRequest Notification `yaml:"pullRequest,omitempty"`
	Commit      Notification `yaml:"commit,omitempty"`
}

type Notification struct {
	// Which Slack channel to notify on a change
	Channel string `yaml:"channel,omitempty"`
	// Which users to tag in the notification
	Users []string `yaml:"users,omitempty"`
}

type ChangeToSend struct {
	Channel           string   // Which Slack channel to send the notification to
	Users             []string // Users to tag in the notification
	ModifiedFiles     []string // Files that were modified
	PullRequestNumber string   // Only set if this is a pull request
	CommitSha         string   // Only set if this is not a pull request, but a commit
	Message           string   // The message to send (Extra part of the Slack notification)
}

func (s ChangeToSend) merge(from ChangeToSend) ChangeToSend {
	s.ModifiedFiles = deduplicate(append(s.ModifiedFiles, from.ModifiedFiles...))
	s.Users = deduplicate(append(s.Users, from.Users...))
	return s
}

func (n *Notification) Merge(n2 *Notification) {
	if n2 == nil {
		return
	}
	if n2.Channel != "" {
		n.Channel = n2.Channel
	}
	n.Users = deduplicate(append(n.Users, n2.Users...))
}

func deduplicate(strings []string) []string {
	seen := map[string]struct{}{}
	ret := make([]string, 0, len(strings))
	for _, s := range strings {
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		ret = append(ret, s)
	}
	return ret
}

func (f *NotificationFile) Merge(notification *NotificationFile) {
	if notification == nil {
		return
	}
	f.PullRequest.Merge(&notification.PullRequest)
	f.Commit.Merge(&notification.Commit)
}

func main() {
	l := Logic{
		Action: githubactions.New(),
	}
	if err := l.Run(context.Background()); err != nil {
		l.Action.Fatalf("Error: %v", err)
	}
}

type Logic struct {
	Action *githubactions.Action
}

func (l *Logic) Run(ctx context.Context) error {
	changedFiles := removeEmptyAndDeDup(strings.Split(l.Action.GetInput("changed-files"), "\n"))
	if len(changedFiles) == 0 {
		return nil
	}
	l.Action.Infof("Changed files: %s", strings.Join(changedFiles, ", "))
	slackClient, err := newSlackClient(l.Action.GetInput("slack-token"), l.Action)
	if err != nil {
		return fmt.Errorf("failed to create slack client: %w", err)
	}
	changes, err := CreateChanges(changedFiles, ChangeTypePullRequest, l.Action.GetInput("pull-request-number"), l.Action.GetInput("commit-sha"))
	if err != nil {
		return fmt.Errorf("failed to create changes: %w", err)
	}
	l.Action.Infof("Changes: %v", changes)
	if err := sendChanges(ctx, slackClient, changes); err != nil {
		return fmt.Errorf("failed to send changes: %w", err)
	}
	return nil
}

type ChangeType int

const (
	ChangeTypePullRequest ChangeType = iota
	ChangeTypeCommit
)

func sendChanges(ctx context.Context, client *slack.Client, changes []ChangeToSend) error {
	for _, change := range changes {
		if err := sendChange(ctx, client, change); err != nil {
			return fmt.Errorf("failed to send change %v: %w", change, err)
		}
	}
	return nil
}

func newSlackClient(token string, action *githubactions.Action) (*slack.Client, error) {
	ret := slack.New(token)
	r, err := ret.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("failed to auth test: %w", err)
	}
	action.Infof("Slack user: %s", r.User)
	return ret, nil
}

func sendChange(ctx context.Context, client *slack.Client, change ChangeToSend) error {
	channel, ts, text, err := client.SendMessageContext(ctx, change.Channel, slack.MsgOptionText(fmt.Sprintf("Files changed: %s", strings.Join(change.ModifiedFiles, ", ")), false))
	if err != nil {
		return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
	}
	_, _, _ = channel, ts, text
	return nil
}

func CreateChanges(changedFiles []string, changeType ChangeType, prNumber string, commitSha string) ([]ChangeToSend, error) {
	// For each changed file, find the notification file
	// Merge them together
	// Create a ChangeToSend for each notification
	// Return the list of changes
	changes := make([]ChangeToSend, 0, len(changedFiles))
	for _, file := range changedFiles {
		notification, err := MergeNotificationsForPath(file)
		if err != nil {
			return nil, fmt.Errorf("failed to merge notifications for path %s: %w", file, err)
		}
		if notification == nil {
			continue
		}
		change := ChangeToSend{
			ModifiedFiles: []string{file},
		}
		switch changeType {
		case ChangeTypePullRequest:
			change.PullRequestNumber = prNumber
			change.Users = notification.PullRequest.Users
			change.Channel = notification.PullRequest.Channel
		case ChangeTypeCommit:
			change.CommitSha = commitSha
			change.Users = notification.Commit.Users
			change.Channel = notification.Commit.Channel
		default:
			panic(fmt.Sprintf("unknown change type %d", changeType))
		}
		if change.Channel == "" {
			continue
		}
		changes = append(changes, change)
	}
	changes = mergeCommonChannelChanges(changes)
	return changes, nil
}

func mergeCommonChannelChanges(changes []ChangeToSend) []ChangeToSend {
	// If there are multiple changes to the same channel, merge them together
	// For this, we can send a single notification to Slack, instead of multiple
	merged := make(map[string]ChangeToSend, len(changes))
	for _, change := range changes {
		if _, exists := merged[change.Channel]; exists {
			merged[change.Channel] = merged[change.Channel].merge(change)
		} else {
			merged[change.Channel] = change
		}
	}
	ret := make([]ChangeToSend, 0, len(merged))
	for _, change := range merged {
		ret = append(ret, change)
	}
	return ret
}

func removeEmptyAndDeDup(split []string) []string {
	ret := make([]string, 0, len(split))
	for _, s := range split {
		if s != "" {
			ret = append(ret, s)
		}
	}
	return deduplicate(ret)
}

const notificationFile = ".notify.yaml"

func MergeNotificationsForPath(path string) (*NotificationFile, error) {
	// Walk up the path, looking for notification files
	// Merge them together
	// Return the merged notification file
	path = filepath.Clean(path)
	var ret NotificationFile
	for {
		notification, err := LoadNotificationForPath(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load notification for path %s: %w", path, err)
		}
		ret.Merge(notification)
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
	return &ret, nil
}

func containsStopFile(path string) bool {
	// Check if the path contains a .notify-stop file
	// If it does, return true
	// Otherwise, return false
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

func LoadNotificationForPath(path string) (*NotificationFile, error) {
	filePath := filepath.Join(path, notificationFile)
	if _, err := os.Stat(filePath); err != nil && os.IsNotExist(err) {
		// File does not exist
		return nil, nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	var ret NotificationFile
	if err := yaml.Unmarshal(content, &ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file %s: %w", filePath, err)
	}
	return &ret, nil
}
