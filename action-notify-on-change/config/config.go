package config

type Config struct {
	GithubToken       string
	SlackToken        string
	CommitSha         string
	RepoOwner         string
	RepoName          string
	BaseBranch        string
	Ref               string
	EventName         string
	RefName           string
	PullRequestNumber int
	ChangeType        ChangeType
}

type ChangeType int

const (
	ChangeTypePullRequest ChangeType = iota
	ChangeTypeCommit
)
