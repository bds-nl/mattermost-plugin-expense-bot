package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

func (p *Plugin) createExpense(userID string, draft *Draft) error {
	expense := &Expense{
		ID:          model.NewId(),
		UserID:      draft.UserID,
		State:       ExpenseStateSubmitted,
		Account:     draft.Data["iban"],
		Name:        draft.Data["name"],
		Amount:      draft.Data["amount"],
		Description: draft.Data["description"],
		FileIDs:     strings.Split(draft.Data["file"], ","),
	}

	message, err := p.formatExpense(expense)
	if err != nil {
		return errors.Wrap(err, "failed to format expense")
	}
	dm := p.sendPinnedDM(userID, message, true)
	if dm == nil {
		return errors.New("failed to create post")
	}

	expense.PostID = dm.Id
	err = p.kvstore.SaveExpense(expense)
	if err != nil {
		return errors.Wrap(err, "failed to save expense")
	}

	return p.sendChannelMessage(expense)
}

func (p *Plugin) formatExpense(expense *Expense) (string, error) {
	var state string
	switch expense.State {
	case ExpenseStateSubmitted:
		state = ":hourglass_flowing_sand: **Submitted**"
	case ExpenseStatePaid:
		state = ":white_check_mark: **Paid**"
	case ExpenseStateRejected:
		state = ":x: **Rejected**"
	}
	links := make([]string, 0, len(expense.FileIDs))
	for _, fileID := range expense.FileIDs {
		file, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			return "", errors.Wrap(appErr, "failed to get file")
		}
		links = append(links, fmt.Sprintf("[%s](%s)", file.Name, fmt.Sprintf("%s/api/v4/files/%s", p.getBaseURL(), file.Id)))
	}
	message := fmt.Sprintf("|Status|%s|\n|-|-|\n|Bank account|%s|\n|Name|%s|\n|Amount|%s|\n|Description|%s|\n|Files|%s|\n",
		state,
		expense.Account,
		expense.Name,
		expense.Amount,
		expense.Description,
		strings.Join(links, ", "),
	)
	return message, nil
}

func (p *Plugin) getBaseURL() string {
	cfg := p.API.GetConfig()
	if cfg == nil || cfg.ServiceSettings.SiteURL == nil {
		p.API.LogWarn("SiteURL is not configured")
		return ""
	}
	return *cfg.ServiceSettings.SiteURL
}

func (p *Plugin) sendChannelMessage(expense *Expense) error {
	channel, appErr := p.API.GetChannel(p.getConfiguration().ChannelID)
	if appErr != nil {
		return errors.Wrap(appErr, "failed to get channel")
	}
	user, appError := p.API.GetUser(expense.UserID)
	if appError != nil {
		return errors.Wrap(appError, "failed to get user")
	}
	title := fmt.Sprintf("**Expense claim from %s**", user.Username)
	message, err := p.formatExpense(expense)
	if err != nil {
		return errors.Wrap(err, "failed to format expense")
	}
	post, appErr := p.API.CreatePost(&model.Post{
		UserId:    p.botID,
		ChannelId: channel.Id,
		Message:   fmt.Sprintf("%s\n\n%s", title, message),
	})
	if appErr != nil {
		return errors.Wrap(appErr, "failed to create post")
	}
	actions := []*model.PostAction{
		{
			Id:    "paid",
			Name:  "Paid",
			Type:  model.PostActionTypeButton,
			Style: "success",
			Integration: &model.PostActionIntegration{
				URL: fmt.Sprintf("/plugins/%s/api/expenses/%s/%s", manifest.Id, expense.ID, ExpenseStatePaid),
			},
		},
		{
			Id:    "reject",
			Name:  "Reject",
			Type:  model.PostActionTypeButton,
			Style: "danger",
			Integration: &model.PostActionIntegration{
				URL: fmt.Sprintf("/plugins/%s/api/expenses/%s/%s", manifest.Id, expense.ID, ExpenseStateRejected),
			},
		},
	}
	attachment := []*model.MessageAttachment{{
		AuthorName: "",
		Actions:    actions,
	}}
	model.ParseMessageAttachment(post, attachment)
	_, appErr = p.API.UpdatePost(post)
	if appErr != nil {
		return errors.Wrap(appErr, "failed to update post")
	}
	return nil
}
