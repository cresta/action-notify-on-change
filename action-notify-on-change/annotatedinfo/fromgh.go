package annotatedinfo

import (
	"context"
	"fmt"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/ghclient"
)

type PopulateFromGh struct {
	ghClient *ghclient.GhClient
	cfg      config.Config
	logger   logger.Logger
	cache    *AnnotatedInfo
}

func NewFromGh(cfg config.Config, ghClient *ghclient.GhClient, logger logger.Logger) *PopulateFromGh {
	return &PopulateFromGh{
		ghClient: ghClient,
		logger:   logger,
		cfg:      cfg,
	}
}

func (p *PopulateFromGh) Populate(ctx context.Context) (*AnnotatedInfo, error) {
	if p.cache != nil {
		p.logger.Infof("Using cached annotated info")
		return p.cache, nil
	}
	// Cannot actually do this with GraphQL: https://github.com/orgs/community/discussions/24496
	// Calculate changed files
	if p.cfg.PullRequestNumber != 0 {
		p.logger.Infof("Appears to be a pull request")
		prInfo, err := p.ghClient.PrInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get pr info: %w", err)
		}
		return p.setCache(&AnnotatedInfo{
			ChangedFiles: prInfo.ChangedFiles,
			LinkToChange: prInfo.PrLink,
			LinkToAuthor: prInfo.AuthorLink,
			PrCreator:    prInfo.PrCreator,
			PrBase:       prInfo.PrBase,
		}), nil
	}
	p.logger.Infof("Appears to be a commit")
	commitInfo, err := p.ghClient.GetCommit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit info: %w", err)
	}
	return p.setCache(&AnnotatedInfo{
		ChangedFiles: commitInfo.ChangedFiles,
		LinkToChange: commitInfo.LinkToChange,
		LinkToAuthor: commitInfo.AuthorLink,
		PrCreator:    commitInfo.AuthorName,
	}), nil
}

func (p *PopulateFromGh) setCache(a *AnnotatedInfo) *AnnotatedInfo {
	p.logger.Infof("changed files: %v", a.ChangedFiles)
	p.cache = a
	return a
}
