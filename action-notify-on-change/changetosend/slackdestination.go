package changetosend

import (
	"context"
	"fmt"
	"strings"

	"github.com/cresta/action-notify-on-change/action-notify-on-change/config"
	"github.com/cresta/action-notify-on-change/action-notify-on-change/logger"
	"github.com/slack-go/slack/slackutilsx"

	"github.com/slack-go/slack"
)

type SlackDestination struct {
	client *slack.Client
	logger logger.Logger
}

func NewSlackDestination(logger logger.Logger, cfg config.Config) (*SlackDestination, error) {
	ret := slack.New(cfg.SlackToken)
	at, err := ret.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("failed to auth test: %w", err)
	}
	logger.Infof("Slack auth test: %+v", at)
	return &SlackDestination{
		client: ret,
		logger: logger,
	}, nil
}

var _ Sender = (*SlackDestination)(nil)

func (s *SlackDestination) SendMessage(ctx context.Context, change ChangeToSend) error {
	s.logger.Infof("Sending slack message for change")
	userMap := s.mapOfUsersByEmail(ctx, change.Users)
	channel, ts, text, err := s.client.SendMessageContext(ctx, change.Channel, createSlackMessage(change), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl(), slack.MsgOptionText("Content change notification", false))
	if err != nil {
		return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
	}
	if len(change.Users) > 0 {
		_, _, _, err = s.client.SendMessageContext(ctx, change.Channel, createUsersMessage(change, userMap), slack.MsgOptionTS(ts), slack.MsgOptionDisableLinkUnfurl(), slack.MsgOptionDisableMediaUnfurl(), slack.MsgOptionText("Content change notification", false))
		if err != nil {
			return fmt.Errorf("failed to send message to channel %s: %w", change.Channel, err)
		}
	}
	_, _, _ = channel, ts, text
	return nil
}

func (s *SlackDestination) mapOfUsersByEmail(ctx context.Context, users []string) map[string]*slack.User {
	ret := make(map[string]*slack.User)
	for _, user := range users {
		user = strings.TrimSpace(user)
		if user == "" {
			continue
		}
		u, err := s.client.GetUserByEmailContext(ctx, user)
		if err != nil {
			s.logger.Infof("failed to get user by email %s: %v", user, err)
			continue
		}
		ret[user] = u
	}
	return ret
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

func createUsersMessage(change ChangeToSend, userMap map[string]*slack.User) slack.MsgOption {
	var blocks []slack.Block
	header := slack.NewTextBlockObject("mrkdwn", "*Subscribers:*", false, false)
	// Mention each user by their email
	allUsers := make([]string, 0, len(change.Users))
	for _, user := range change.Users {
		userText := slackutilsx.EscapeMessage(user)
		if u, ok := userMap[user]; ok {
			userText = fmt.Sprintf("<@%s|%s>", u.ID, slackutilsx.EscapeMessage(u.Name))
		}
		allUsers = append(allUsers, userText)
	}
	for _, group := range change.Groups {
		allUsers = append(allUsers, fmt.Sprintf("@%s", group))
	}
	txtBlock := slack.NewTextBlockObject("mrkdwn", strings.Join(allUsers, ", "), false, false)
	blocks = append(blocks, slack.NewSectionBlock(header, []*slack.TextBlockObject{txtBlock}, nil))
	return slack.MsgOptionBlocks(blocks...)
}

func createSlackMessage(change ChangeToSend) slack.MsgOption {
	var blocks []slack.Block
	// https://api.slack.com/reference/block-kit/composition-objects#text
	blocks = append(blocks,
		slack.NewHeaderBlock(slack.NewTextBlockObject("plain_text", "Content change notification", false, false)),
	)
	sourceText := changeSourceText(change)
	var sourceTextBlock *slack.TextBlockObject
	if sourceText != "" {
		if change.LinkToChange != "" {
			sourceText = slackutilsx.EscapeMessage(sourceText)
			sourceTextBlock = slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Source:*\n<%s|%s>", change.LinkToChange, sourceText), false, false)
		} else {
			sourceTextBlock = slack.NewTextBlockObject("plain_text", fmt.Sprintf("Source: %s", sourceText), false, false)
		}
	}

	var creatorTextBlock *slack.TextBlockObject
	if change.Creator != "" {
		if change.LinkToAuthor != "" {
			changeCreator := slackutilsx.EscapeMessage(change.Creator)
			creatorTextBlock = slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Author:*\n<%s|%s>", change.LinkToAuthor, changeCreator), false, false)
		} else {
			creatorTextBlock = slack.NewTextBlockObject("plain_text", fmt.Sprintf("Author: %s", change.Creator), false, false)
		}
	}
	blocks = append(blocks, slack.NewSectionBlock(nil, []*slack.TextBlockObject{sourceTextBlock, creatorTextBlock}, nil))
	if len(change.ModifiedFiles) > 0 {
		header := slack.NewTextBlockObject("mrkdwn", "*Modified files:*", false, false)
		filesMonospace := make([]string, 0, len(change.ModifiedFiles))
		for _, file := range change.ModifiedFiles {
			filesMonospace = append(filesMonospace, file)
		}
		monoTextBlock := slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("\n```\n%s\n```\n", strings.Join(filesMonospace, "\n")), false, false)
		blocks = append(blocks, slack.NewSectionBlock(header, []*slack.TextBlockObject{monoTextBlock}, nil))
	}
	if change.Message != "" {
		header := slack.NewTextBlockObject("mrkdwn", "*Custom Message:*", false, false)
		blocks = append(blocks, slack.NewSectionBlock(
			header, []*slack.TextBlockObject{
				slack.NewTextBlockObject("mrkdwn", change.Message, false, false),
			}, nil))
	}
	return slack.MsgOptionBlocks(blocks...)
}
