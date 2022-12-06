package populatefromgh

import (
	"context"
	"fmt"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/annotatedinfo"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
)

type Populator struct {
	ghClient *ghclient.GhClient
	cfg      *config.Config
}

func New(cfg *config.Config, ghClient *ghclient.GhClient) *Populator {
	return &Populator{
		ghClient: ghClient,
		cfg:      cfg,
	}
}

func (p *Populator) Populate(ctx context.Context) (*annotatedinfo.AnnotatedInfo, error) {
	// Cannot actually do this with GraphQL: https://github.com/orgs/community/discussions/24496
	// Calculate changed files
	if p.cfg.PullRequestNumber != 0 {
		prInfo, err := p.ghClient.PrInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get pr info: %w", err)
		}
		return &annotatedinfo.AnnotatedInfo{
			ChangedFiles: prInfo.ChangedFiles,
			LinkToChange: prInfo.PrLink,
			LinkToAuthor: prInfo.AuthorLink,
			PrCreator:    prInfo.PrCreator,
			PrBase:       prInfo.PrBase,
		}, nil
	} else {
		commitInfo, err := p.ghClient.GetCommit(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get commit info: %w", err)
		}
		return &annotatedinfo.AnnotatedInfo{
			ChangedFiles: commitInfo.ChangedFiles,
			LinkToChange: commitInfo.LinkToChange,
			LinkToAuthor: commitInfo.AuthorLink,
			PrCreator:    commitInfo.AuthorName,
		}, nil
	}
}
