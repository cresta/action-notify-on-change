package notificationfile

import (
	"fmt"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/stringhelper"
	"html/template"
	"strings"
)

type NotificationFile struct {
	PullRequest     Notification `yaml:"pullRequest,omitempty"`
	Commit          Notification `yaml:"commit,omitempty"`
	PrettyName      []string     `yaml:"prettyName,omitempty"`
	MessageTemplate string       `yaml:"messageTemplate,omitempty"`
	// Parent is the notification file in the Parent directory. If there is none, it's an empty file.
	Parent      *NotificationFile `yaml:"-"` // This is used to allow us to merge the Parent with the child
	ChangedFile string            `yaml:"-"` // Which files were changed that caused this notification file to be used
}

type ChangeType int

const (
	ChangeTypePullRequest ChangeType = iota
	ChangeTypeCommit
)

type Notification struct {
	// Which Slack channel to notify on a change
	Channel string `yaml:"channel,omitempty"`
	// Which users to tag in the notification
	Users []string `yaml:"users,omitempty"`
}

func (f *NotificationFile) ProcessTemplate() (string, error) {
	if f == nil {
		return "", nil
	}
	parentTemplate, err := f.Parent.ProcessTemplate()
	if err != nil {
		return "", fmt.Errorf("failed to process Parent template: %w", err)
	}
	t, err := template.New("message").Parse(f.MessageTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", f.MessageTemplate, err)
	}
	var b strings.Builder
	if err := t.Execute(&b, f); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", f.MessageTemplate, err)
	}
	ret := b.String()
	if parentTemplate != "" {
		ret = parentTemplate + " - " + ret
	}
	return ret, nil
}

func (f *NotificationFile) AllUsers(changeType ChangeType) []string {
	if f == nil {
		return nil
	}
	users := f.Users(changeType)
	if f.Parent != nil {
		users = append(users, f.Parent.AllUsers(changeType)...)
	}
	return stringhelper.Deduplicate(users)
}

func (f *NotificationFile) Users(changeType ChangeType) []string {
	if f == nil {
		return nil
	}
	switch changeType {
	case ChangeTypeCommit:
		return f.Commit.Users
	case ChangeTypePullRequest:
		return f.PullRequest.Users
	default:
		panic("unknown change type")
	}
}

func (f *NotificationFile) Channel(changeType ChangeType) string {
	if f == nil {
		return ""
	}
	switch changeType {
	case ChangeTypeCommit:
		if f.Commit.Channel != "" {
			return f.Commit.Channel
		}
		return f.Parent.Channel(changeType)
	case ChangeTypePullRequest:
		if f.PullRequest.Channel != "" {
			return f.PullRequest.Channel
		}
		return f.Parent.Channel(changeType)
	default:
		panic("unknown change type")
	}
}

func (f *NotificationFile) String() string {
	return fmt.Sprintf("NotificationFile{PullRequest:%v,Commit:%v,PrettyName:%v,MessageTemplate:%v,Parent:%v,ChangedFile:%v}", f.PullRequest, f.Commit, f.PrettyName, f.MessageTemplate, f.Parent, f.ChangedFile)
}
