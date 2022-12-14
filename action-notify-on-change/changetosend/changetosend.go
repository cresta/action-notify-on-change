package changetosend

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/stringhelper"
)

type ChangeToSend struct {
	Channel           string    // Which Slack channel to send the notification to
	Users             []string  // Users to tag in the notification
	Groups            []string  // Groups to tag in the notification
	ModifiedFiles     []string  // Files that were modified
	PullRequestNumber int       // Only set if this is a pull request
	Branch            string    // Only set if this is a commit in a branch
	CommitSha         string    // Only set if this is not a pull request, but a commit
	Creator           string    // The user that created the pull request or commit
	Timestamp         time.Time // The time the pull request or commit was created
	LinkToChange      string    // Link to the pull request or commit
	LinkToAuthor      string    // Link to the user that created the pull request or commit
	Messages          []string  // The message to send (Extra part of the Slack notification)
}

type Sender interface {
	SendMessage(ctx context.Context, change ChangeToSend) error
}

func SendMessagesInParallel(ctx context.Context, sender Sender, changes []ChangeToSend) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for _, change := range changes {
		change := change
		eg.Go(func() error {
			return sender.SendMessage(egCtx, change)
		})
	}
	return eg.Wait()
}

func (s ChangeToSend) merge(from ChangeToSend) ChangeToSend {
	s.ModifiedFiles = stringhelper.Deduplicate(append(s.ModifiedFiles, from.ModifiedFiles...))
	s.Users = stringhelper.Deduplicate(append(s.Users, from.Users...))
	s.Messages = append(s.Messages, from.Messages...)
	return s
}

func MergeCommon(changes []ChangeToSend) []ChangeToSend {
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
