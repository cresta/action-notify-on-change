package slackdestination

import (
	"context"
	"fmt"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/changetosend"
	"github.com/slack-go/slack"
)

type SlackDestination struct {
	client *slack.Client
}

func New(cfg config.Config) (*SlackDestination, error) {
	ret := slack.New(cfg.SlackToken)
	_, err := ret.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("failed to auth test: %w", err)
	}
	return &SlackDestination{
		client: ret,
	}, nil
}

var _ changetosend.Sender = (*SlackDestination)(nil)

func (s *SlackDestination) SendMessage(ctx context.Context, change changetosend.ChangeToSend) error {
	channel, ts, text, err := s.client.SendMessageContext(ctx, change.Channel, createSlackMessage(change), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl())
	if err != nil {
		return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
	}
	_, _, _ = channel, ts, text
	return nil
}

func changeSourceText(change changetosend.ChangeToSend) string {
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

func createSlackMessage(change changetosend.ChangeToSend) slack.MsgOption {
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
