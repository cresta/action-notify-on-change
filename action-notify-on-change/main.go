package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v48/github"
	"github.com/sethvargo/go-githubactions"
	"github.com/shurcooL/githubv4"
	"github.com/slack-go/slack"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/yaml"
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
	return deduplicate(users)
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

type Notification struct {
	// Which Slack channel to notify on a change
	Channel string `yaml:"channel,omitempty"`
	// Which users to tag in the notification
	Users []string `yaml:"users,omitempty"`
}

type ChangeToSend struct {
	Channel           string    // Which Slack channel to send the notification to
	Users             []string  // Users to tag in the notification
	ModifiedFiles     []string  // Files that were modified
	PullRequestNumber int       // Only set if this is a pull request
	Branch            string    // Only set if this is a commit in a branch
	CommitSha         string    // Only set if this is not a pull request, but a commit
	Creator           string    // The user that created the pull request or commit
	Timestamp         time.Time // The time the pull request or commit was created
	LinkToChange      string    // Link to the pull request or commit
	LinkToAuthor      string    // Link to the user that created the pull request or commit
	Message           string    // The message to send (Extra part of the Slack notification)
}

func (s ChangeToSend) merge(from ChangeToSend) ChangeToSend {
	s.ModifiedFiles = deduplicate(append(s.ModifiedFiles, from.ModifiedFiles...))
	s.Users = deduplicate(append(s.Users, from.Users...))
	s.Message = s.Message + "\n" + from.Message
	return s
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

func main() {
	l := Logic{
		Action: githubactions.New(),
	}
	if err := l.Run(context.Background()); err != nil {
		l.Action.Fatalf("Error: %v", err)
	}
}

type ActionStub interface {
	Fatalf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	GetInput(name string) string
	Context() (*githubactions.GitHubContext, error)
}

var _ ActionStub = &githubactions.Action{}

type Logic struct {
	Action ActionStub
}

func (l *Logic) Run(ctx context.Context) error {
	l.Action.Infof("Starting action-notify-on-change")
	// TODO: The graphql client is currently unused
	// ghGraphqlClient, err := NewGithubGraphQLClient(ctx, l.Action.GetInput("github-token"))
	// if err != nil {
	//   return fmt.Errorf("failed to create github client: %w", err)
	// }
	ghClient, err := NewGithubClient(ctx, l.Action.GetInput("github-token"))
	if err != nil {
		return fmt.Errorf("failed to create github client: %w", err)
	}
	l.Action.Infof("Created github client")
	ghCtx, err := l.Action.Context()
	if err != nil {
		return fmt.Errorf("failed to get github context: %w", err)
	}
	input, err := CalculateInput(ctx, ghCtx, ghClient)
	if err != nil {
		return fmt.Errorf("failed to calculate input: %w", err)
	}
	l.Action.Infof("Calculated input: %+v", input)
	changedFiles := removeEmptyAndDeDup(input.ChangedFiles)
	if len(changedFiles) == 0 {
		l.Action.Infof("No changed files, skipping")
		return nil
	}
	l.Action.Infof("Changed files: %s", strings.Join(changedFiles, ", "))
	slackClient, err := newSlackClient(l.Action.GetInput("slack-token"), l.Action)
	if err != nil {
		return fmt.Errorf("failed to create slack client: %w", err)
	}
	l.Action.Infof("Created slack client")
	changes, err := CreateChanges(ctx, changedFiles, ChangeTypePullRequest, input.PullRequestNumber, input.CommitSha, l.Action, input.RepoReference(), ghClient, input)
	if err != nil {
		return fmt.Errorf("failed to create changes: %w", err)
	}
	l.Action.Infof("Changes: %v", changes)
	if err := sendChanges(ctx, slackClient, changes); err != nil {
		return fmt.Errorf("failed to send changes: %w", err)
	}
	l.Action.Infof("Sent changes")
	return nil
}

type ChangeInput struct {
	ChangedFiles      []string
	LinkToChange      string
	LinkToAuthor      string
	BaseBranch        string
	PullRequestNumber int
	CommitSha         string
	Owner             string
	Name              string
	Creator           string
}

func (i ChangeInput) RepoReference() *RepoReference {
	return &RepoReference{
		Owner: i.Owner,
		Repo:  i.Name,
		Sha:   i.CommitSha,
	}
}

func NewGithubClient(ctx context.Context, token string) (*github.Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)
	_, _, err := client.Zen(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query github zen: %w", err)
	}
	return client, nil
}

func NewGithubGraphQLClient(ctx context.Context, token string) (*githubv4.Client, error) {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(ctx, src)

	client := githubv4.NewClient(httpClient)
	// Test query to make sure the token works
	var query struct {
		Viewer struct {
			Login githubv4.String
		}
	}
	err := client.Query(ctx, &query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query github viewer: %w", err)
	}
	return client, nil
}

func CalculateInput(ctx context.Context, ghCtx *githubactions.GitHubContext, client *github.Client) (*ChangeInput, error) {
	owner, name := ghCtx.Repo()
	ret := &ChangeInput{
		Owner:      owner,
		Name:       name,
		CommitSha:  ghCtx.SHA,
		BaseBranch: ghCtx.BaseRef,
	}
	if ghCtx.EventName == "pull_request" {
		rgx := regexp.MustCompile(`^refs/pull/([0-9]+)/merge$`)
		matches := rgx.FindStringSubmatch(ghCtx.Ref)
		if len(matches) != 2 {
			return nil, fmt.Errorf("failed to parse pull request number from ref %s", ghCtx.Ref)
		}
		var err error
		ret.PullRequestNumber, err = strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse pull request number from ref %s: %w", ghCtx.Ref, err)
		}
	}
	// Cannot actually do this with GraphQL: https://github.com/orgs/community/discussions/24496
	// Calculate changed files
	if ret.PullRequestNumber != 0 {
		var opts github.ListOptions
		for {
			prInfo, resp, err := client.PullRequests.Get(ctx, owner, name, ret.PullRequestNumber)
			if err != nil {
				return nil, fmt.Errorf("failed to get pull request info: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get pull request info: %d", resp.StatusCode)
			}
			if ret.LinkToChange == "" {
				ret.LinkToChange = prInfo.GetHTMLURL()
			}
			if ret.LinkToAuthor == "" {
				ret.LinkToAuthor = prInfo.User.GetHTMLURL()
			}
			if ret.Creator == "" {
				ret.Creator = prInfo.User.GetLogin()
			}
			if ret.BaseBranch == "" {
				ret.BaseBranch = prInfo.GetBase().GetRef()
			}
			files, resp, err := client.PullRequests.ListFiles(ctx, owner, name, ret.PullRequestNumber, &opts)
			if err != nil {
				return nil, fmt.Errorf("failed to list pull request files: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to list pull request files: %s", resp.Status)
			}
			for _, file := range files {
				ret.ChangedFiles = append(ret.ChangedFiles, file.GetFilename())
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
	} else {
		var opts github.ListOptions
		for {
			commit, resp, err := client.Repositories.GetCommit(ctx, owner, name, ret.CommitSha, &opts)
			if ret.CommitSha == "" {
				ret.CommitSha = commit.GetSHA()
			}
			if ret.Creator == "" {
				ret.Creator = commit.GetAuthor().GetLogin()
			}
			if ret.LinkToChange == "" {
				ret.LinkToChange = commit.GetHTMLURL()
			}
			if ret.LinkToAuthor == "" {
				ret.LinkToAuthor = commit.GetAuthor().GetHTMLURL()
			}
			if ret.BaseBranch == "" {
				ret.BaseBranch = ghCtx.RefName
			}
			if err != nil {
				return nil, fmt.Errorf("failed to get commit: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get commit: %s", resp.Status)
			}
			for _, file := range commit.Files {
				ret.ChangedFiles = append(ret.ChangedFiles, file.GetFilename())
			}
			if resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
	}
	return ret, nil
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

func newSlackClient(token string, action ActionStub) (*slack.Client, error) {
	ret := slack.New(token)
	r, err := ret.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("failed to auth test: %w", err)
	}
	action.Infof("Slack user: %s", r.User)
	return ret, nil
}

func sendChange(ctx context.Context, client *slack.Client, change ChangeToSend) error {
	channel, ts, text, err := client.SendMessageContext(ctx, change.Channel, createSlackMessage(change), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
	if err != nil {
		return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
	}
	_, _, _ = channel, ts, text
	return nil
}

func changeSourceText(change ChangeToSend) string {
	switch {
	case change.PullRequestNumber != 0:
		return fmt.Sprintf("Pull request #%d", change.PullRequestNumber)
	case change.Branch != "":
		return fmt.Sprintf("Branch %s", change.Branch)
	case change.CommitSha != "":
		return fmt.Sprintf("Commit %s", change.CommitSha)
	}
	return ""
}

func createSlackMessage(change ChangeToSend) slack.MsgOption {
	var blocks []slack.Block
	// https://api.slack.com/reference/block-kit/composition-objects#text
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("plain_text", "Content change notification", false, false), nil, nil),
	)
	sourceText := changeSourceText(change)
	if sourceText != "" {
		var field *slack.TextBlockObject
		if change.LinkToChange != "" {
			field = slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Source: <%s|%s>", change.LinkToChange, sourceText), false, false)
		} else {
			field = slack.NewTextBlockObject("plain_text", fmt.Sprintf("Source: %s", sourceText), false, false)
		}
		blocks = append(blocks,
			slack.NewSectionBlock(field, nil, nil),
		)
	}
	if change.Creator != "" {
		var field *slack.TextBlockObject
		if change.LinkToAuthor != "" {
			field = slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("Author: <%s|%s>", change.LinkToAuthor, change.Creator), false, false)
		} else {
			field = slack.NewTextBlockObject("plain_text", fmt.Sprintf("Author: %s", change.Creator), false, false)
		}
		blocks = append(blocks, slack.NewSectionBlock(
			field,
			nil, nil,
		))
	}
	if len(change.ModifiedFiles) > 0 {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Modified files:", false, false),
			nil, nil,
		))
		for _, file := range change.ModifiedFiles {
			blocks = append(blocks, slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", file, false, false),
				nil, nil,
			))
		}
	}
	if change.Message != "" {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", change.Message, false, false),
			nil, nil,
		))
	}
	return slack.MsgOptionBlocks(blocks...)
}

func CreateChangesForFile(ctx context.Context, file string, changeType ChangeType, prNumber int, commitSha string, a ActionStub, ref *RepoReference, client *github.Client, input *ChangeInput) (*ChangeToSend, error) {
	notification, err := MergeNotificationsForPath(ctx, file, a, ref, client)
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

	change := ChangeToSend{
		ModifiedFiles: []string{file},
		Message:       notifMsg,
		CommitSha:     commitSha,
		Creator:       input.Creator,
		LinkToChange:  input.LinkToChange,
		LinkToAuthor:  input.LinkToAuthor,
	}
	switch changeType {
	case ChangeTypePullRequest:
		change.PullRequestNumber = prNumber
		change.Users = notification.AllUsers(changeType)
		change.Channel = notification.Channel(changeType)
	case ChangeTypeCommit:
		change.Users = notification.AllUsers(changeType)
		change.Channel = notification.Channel(changeType)
	default:
		panic(fmt.Sprintf("unknown change type %d", changeType))
	}
	if change.Channel == "" {
		return nil, nil
	}
	return &change, nil
}

func CreateChanges(ctx context.Context, changedFiles []string, changeType ChangeType, prNumber int, commitSha string, a ActionStub, ref *RepoReference, client *github.Client, input *ChangeInput) ([]ChangeToSend, error) {
	// For each changed file, find the notification file
	// Merge them together
	// Create a ChangeToSend for each notification
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
			change, err := CreateChangesForFile(egCtx, file, changeType, prNumber, commitSha, a, ref, client, input)
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
	return mergeCommonChannelChanges(ret), nil
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

const notificationFile = ".action-notify-on-change.yaml"

func MergeNotificationsForPath(ctx context.Context, path string, a ActionStub, ref *RepoReference, client *github.Client) (*NotificationFile, error) {
	// Walk up the path, looking for notification files
	// Merge them together
	// Return the merged notification file
	path = filepath.Clean(path)
	rootPath := path
	type loadRetVal struct {
		idx          int
		notification *NotificationFile
	}
	var i int
	eg, egCtx := errgroup.WithContext(ctx)
	allRetValues := make([]loadRetVal, 0, 10)
	var allRetValuesMu sync.Mutex
	for i = 0; ; i++ {
		a.Infof("Looking for notification file in %s", path)
		idx := i
		loadPath := path
		eg.Go(func() error {
			notification, err := LoadNotificationForPath(egCtx, loadPath, ref, client)
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
		allRetValues[idx].notification.Parent = allRetValues[idx-1].notification
	}
	return ret, nil
}

func containsStopFile(path string) bool {
	// Check if the path contains a .notify-stop file
	// If it does, return true
	// Otherwise, return false
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

type RepoReference struct {
	Owner string
	Repo  string
	Sha   string
}

func LoadNotificationForPath(ctx context.Context, path string, ref *RepoReference, client *github.Client) (*NotificationFile, error) {
	filePath := filepath.Join(path, notificationFile)
	// Note: If we get throttled here, we can cache results
	fc, dc, res, err := client.Repositories.GetContents(ctx, ref.Owner, ref.Repo, filePath, &github.RepositoryContentGetOptions{Ref: ref.Sha})
	if err != nil {
		if res != nil && res.StatusCode == http.StatusNotFound {
			return &NotificationFile{}, nil
		}
		return nil, fmt.Errorf("failed to get contents for %s: %w", path, err)
	}
	if res.StatusCode == http.StatusNotFound {
		return &NotificationFile{}, nil
	}
	if dc != nil {
		// A directory: ignore it
		return &NotificationFile{}, nil
	}
	if fc == nil {
		return nil, fmt.Errorf("failed to get contents for %s: no file contents", path)
	}
	fileContent, err := fc.GetContent()
	if err != nil {
		return nil, fmt.Errorf("failed to get file content for %s: %w", filePath, err)
	}
	var ret NotificationFile
	if err := yaml.Unmarshal([]byte(fileContent), &ret); err != nil {
		return nil, fmt.Errorf("failed to unmarshal file %s: %w", filePath, err)
	}
	return &ret, nil
}
