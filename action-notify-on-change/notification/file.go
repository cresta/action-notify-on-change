package notification

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/stringhelper"
)

type File struct {
	PullRequest     Notification `yaml:"pullRequest,omitempty"`
	Commit          Notification `yaml:"commit,omitempty"`
	PrettyName      []string     `yaml:"prettyName,omitempty"`
	MessageTemplate string       `yaml:"messageTemplate,omitempty"`
	// Parent is the notification file in the Parent directory. If there is none, it's an empty file.
	Parent      *File  `yaml:"-"` // This is used to allow us to merge the Parent with the child
	ChangedFile string `yaml:"-"` // Which files were changed that caused this notification file to be used
}

type Notification struct {
	// Which Slack channel to notify on a change
	Channel string `yaml:"channel,omitempty"`
	// Which users to tag in the notification
	Users           []string `yaml:"users,omitempty"`
	Groups          []string `yaml:"groups,omitempty"`
	MessageTemplate string   `yaml:"messageTemplate,omitempty"`
}

func (f *File) ProcessTemplate(changeType config.ChangeType) (string, error) {
	if f == nil {
		return "", nil
	}
	parentTemplate, err := f.Parent.ProcessTemplate(changeType)
	if err != nil {
		return "", fmt.Errorf("failed to process Parent template: %w", err)
	}
	var messageTemplate string
	if changeType == config.ChangeTypeCommit {
		messageTemplate = f.Commit.MessageTemplate
	} else {
		messageTemplate = f.PullRequest.MessageTemplate
	}
	if messageTemplate == "" {
		messageTemplate = f.MessageTemplate
	}
	if messageTemplate == "" {
		return parentTemplate, nil
	}
	t, err := template.New("message").Parse(messageTemplate)
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

func (f *File) AllUsers(changeType config.ChangeType) []string {
	if f == nil {
		return nil
	}
	users := f.Users(changeType)
	if f.Parent != nil {
		users = append(users, f.Parent.AllUsers(changeType)...)
	}
	return stringhelper.Deduplicate(users)
}

func (f *File) AllGroups(changeType config.ChangeType) []string {
	if f == nil {
		return nil
	}
	groups := f.Groups(changeType)
	if f.Parent != nil {
		groups = append(groups, f.Parent.AllGroups(changeType)...)
	}
	return stringhelper.Deduplicate(groups)
}

func (f *File) Users(changeType config.ChangeType) []string {
	if f == nil {
		return nil
	}
	switch changeType {
	case config.ChangeTypeCommit:
		return f.Commit.Users
	case config.ChangeTypePullRequest:
		return f.PullRequest.Users
	default:
		panic("unknown change type")
	}
}

func (f *File) Groups(changeType config.ChangeType) []string {
	if f == nil {
		return nil
	}
	switch changeType {
	case config.ChangeTypeCommit:
		return f.Commit.Groups
	case config.ChangeTypePullRequest:
		return f.PullRequest.Groups
	default:
		panic("unknown change type")
	}
}

func (f *File) Channel(changeType config.ChangeType) string {
	if f == nil {
		return ""
	}
	switch changeType {
	case config.ChangeTypeCommit:
		if f.Commit.Channel != "" {
			return f.Commit.Channel
		}
		return f.Parent.Channel(changeType)
	case config.ChangeTypePullRequest:
		if f.PullRequest.Channel != "" {
			return f.PullRequest.Channel
		}
		return f.Parent.Channel(changeType)
	default:
		panic("unknown change type")
	}
}

func (f *File) String() string {
	return fmt.Sprintf("File{PullRequest:%v,Commit:%v,PrettyName:%v,MessageTemplate:%v,Parent:%v,ChangedFile:%v}", f.PullRequest, f.Commit, f.PrettyName, f.MessageTemplate, f.Parent, f.ChangedFile)
}
