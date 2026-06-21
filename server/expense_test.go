package main

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func stubBaseURL(api *plugintest.API, url string) {
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{SiteURL: new(url)},
	}).Maybe()
}

func stubFileInfo(api *plugintest.API, id, name string) {
	api.On("GetFileInfo", id).Return(&model.FileInfo{Id: id, Name: name}, nil).Maybe()
}

func TestFormatExpenseRendersTableAndFileLinks(t *testing.T) {
	p, api, _, _ := newTestPlugin()
	stubBaseURL(api, "https://chat.example.com")
	stubFileInfo(api, "file1", "receipt.pdf")

	expense := &Expense{
		State:       ExpenseStateSubmitted,
		Account:     validIBANPrinted,
		Name:        "Jane Doe",
		Amount:      "42.00",
		Description: "Train ticket",
		FileIDs:     []string{"file1"},
	}

	msg, err := p.formatExpense(expense)
	require.NoError(t, err)

	assert.Contains(t, msg, "Submitted")
	assert.Contains(t, msg, validIBANPrinted)
	assert.Contains(t, msg, "Jane Doe")
	assert.Contains(t, msg, "42.00")
	assert.Contains(t, msg, "Train ticket")
	assert.Contains(t, msg, "[receipt.pdf](https://chat.example.com/api/v4/files/file1)")
}

func TestFormatExpenseStateEmoji(t *testing.T) {
	p, api, _, _ := newTestPlugin()
	stubBaseURL(api, "https://chat.example.com")

	cases := map[string]string{
		ExpenseStateSubmitted: ":hourglass_flowing_sand:",
		ExpenseStatePaid:      ":white_check_mark:",
		ExpenseStateRejected:  ":x:",
	}
	for state, emoji := range cases {
		msg, err := p.formatExpense(&Expense{State: state, FileIDs: []string{}})
		require.NoError(t, err)
		assert.Contains(t, msg, emoji, "state %s", state)
	}
}

func TestFormatExpenseFailsWhenFileMissing(t *testing.T) {
	p, api, _, _ := newTestPlugin()
	stubBaseURL(api, "https://chat.example.com")
	api.On("GetFileInfo", "missing").Return(nil, model.NewAppError("GetFileInfo", "not found", nil, "", 404))

	_, err := p.formatExpense(&Expense{State: ExpenseStateSubmitted, FileIDs: []string{"missing"}})
	assert.Error(t, err)
}

// TestCreateExpensePersistsAndPostsToChannel exercises the full submission tail:
// it should persist the expense, post a pinned DM summary, and post to the
// configured channel.
func TestCreateExpensePersistsAndPostsToChannel(t *testing.T) {
	p, api, kv, _ := newTestPlugin()
	p.setConfiguration(&configuration{ChannelID: "expense-channel"})
	stubBaseURL(api, "https://chat.example.com")
	stubFileInfo(api, "file1", "receipt.pdf")
	api.On("GetChannel", "expense-channel").
		Return(&model.Channel{Id: "expense-channel"}, nil)
	api.On("GetUser", "u1").
		Return(&model.User{Id: "u1", Username: "jane"}, nil)
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).
		Return(&model.Post{}, nil)

	draft := &Draft{
		UserID: "u1",
		State:  DraftStateAskFile,
		Data: map[string]string{
			"iban":        validIBANPrinted,
			"name":        "Jane Doe",
			"amount":      "42.00",
			"description": "Train ticket",
			"file":        "file1",
		},
	}

	err := p.createExpense("u1", draft)
	require.NoError(t, err)

	require.Len(t, kv.expenses, 1)
	for _, e := range kv.expenses {
		assert.Equal(t, ExpenseStateSubmitted, e.State)
		assert.Equal(t, "u1", e.UserID)
		assert.Equal(t, []string{"file1"}, e.FileIDs)
		assert.NotEmpty(t, e.PostID, "PostID should be set from the pinned DM")
	}
}
