package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-github/v48/github"
	"github.com/sethvargo/go-githubactions"
	"github.com/shurcooL/githubv4"
	"github.com/slack-go/slack"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
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
	PullRequestNumber int      // Only set if this is a pull request
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
	changes, err := CreateChanges(ctx, changedFiles, ChangeTypePullRequest, input.PullRequestNumber, input.CommitSha, l.Action, input.RepoReference(), ghClient)
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
	PullRequestNumber int
	CommitSha         string
	Owner             string
	Name              string
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
		Owner:     owner,
		Name:      name,
		CommitSha: ghCtx.SHA,
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
	channel, ts, text, err := client.SendMessageContext(ctx, change.Channel, slack.MsgOptionText(fmt.Sprintf("Files changed: %s", strings.Join(change.ModifiedFiles, ", ")), false))
	if err != nil {
		return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
	}
	_, _, _ = channel, ts, text
	return nil
}

func CreateChangesForFile(ctx context.Context, file string, changeType ChangeType, prNumber int, commitSha string, a ActionStub, ref *RepoReference, client *github.Client) (*ChangeToSend, error) {
	notification, err := MergeNotificationsForPath(ctx, file, a, ref, client)
	if err != nil {
		return nil, fmt.Errorf("failed to merge notifications for path %s: %w", file, err)
	}
	if notification == nil {
		return nil, nil
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
		return nil, nil
	}
	return &change, nil
}

func CreateChanges(ctx context.Context, changedFiles []string, changeType ChangeType, prNumber int, commitSha string, a ActionStub, ref *RepoReference, client *github.Client) ([]ChangeToSend, error) {
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
			change, err := CreateChangesForFile(egCtx, file, changeType, prNumber, commitSha, a, ref, client)
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
	type loadRetVal struct {
		idx          int
		notification *NotificationFile
	}
	var i int
	eg, egCtx := errgroup.WithContext(ctx)
	allRetValues := make([]loadRetVal, 0, i)
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
	var ret NotificationFile
	for _, v := range allRetValues {
		ret.Merge(v.notification)
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

type RepoReference struct {
	Owner string
	Repo  string
	Sha   string
}

func LoadNotificationForPath(ctx context.Context, path string, ref *RepoReference, client *github.Client) (*NotificationFile, error) {
	filePath := filepath.Join(path, notificationFile)
	// Note: If we get throttled here, we can cache results
	fc, dc, res, err := client.Repositories.GetContents(ctx, ref.Owner, ref.Repo, path, &github.RepositoryContentGetOptions{Ref: ref.Sha})
	if err != nil {
		if res.StatusCode == 404 {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get contents for %s: %w", path, err)
	}
	if dc != nil {
		// A directory: ignore it
		return nil, nil
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
