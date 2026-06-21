package main

import (
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/mock"
)

// fakeKVStore is an in-memory implementation of KVStore for tests, avoiding the
// need to mock the plugin KV API for every state transition.
type fakeKVStore struct {
	defaults map[string]*UserDefaults
	drafts   map[string]*Draft
	expenses map[string]*Expense
}

func newFakeKVStore() *fakeKVStore {
	return &fakeKVStore{
		defaults: map[string]*UserDefaults{},
		drafts:   map[string]*Draft{},
		expenses: map[string]*Expense{},
	}
}

func (f *fakeKVStore) GetUserDefaults(userID string) (*UserDefaults, error) {
	return f.defaults[userID], nil
}

func (f *fakeKVStore) SaveUserDefaults(user *UserDefaults) error {
	f.defaults[user.UserID] = user
	return nil
}

func (f *fakeKVStore) GetDraft(userID string) (*Draft, error) {
	return f.drafts[userID], nil
}

func (f *fakeKVStore) SaveDraft(userID string, draft *Draft) error {
	f.drafts[userID] = draft
	return nil
}

func (f *fakeKVStore) DeleteDraft(userID string) error {
	delete(f.drafts, userID)
	return nil
}

func (f *fakeKVStore) GetExpense(expenseID string) (*Expense, error) {
	return f.expenses[expenseID], nil
}

func (f *fakeKVStore) SaveExpense(expense *Expense) error {
	f.expenses[expense.ID] = expense
	return nil
}

// newTestPlugin wires up a Plugin with a mocked API and the in-memory KV store.
// Direct-message plumbing (GetDirectChannel + CreatePost) is stubbed so that
// every sendDM call records the outgoing message into *messages.
func newTestPlugin() (*Plugin, *plugintest.API, *fakeKVStore, *[]string) {
	api := &plugintest.API{}
	kv := newFakeKVStore()
	messages := &[]string{}

	p := &Plugin{kvstore: kv, botID: "bot-id"}
	p.SetAPI(api)

	api.On("GetDirectChannel", "bot-id", mock.AnythingOfType("string")).
		Return(&model.Channel{Id: "dm-channel"}, nil).Maybe()

	api.On("CreatePost", mock.AnythingOfType("*model.Post")).
		Return(func(post *model.Post) *model.Post {
			*messages = append(*messages, post.Message)
			post.Id = model.NewId()
			return post
		}, nil).Maybe()

	return p, api, kv, messages
}

// directMessagePost builds a post that MessageHasBeenPosted treats as a DM to
// the bot, after the channel/member lookups are stubbed by stubDirectChannel.
func directMessagePost(userID, message string, fileIDs ...string) *model.Post {
	return &model.Post{
		UserId:    userID,
		ChannelId: "user-dm",
		Message:   message,
		FileIds:   fileIDs,
	}
}

// stubDirectChannel makes MessageHasBeenPosted accept posts in "user-dm" as DMs
// the bot is a member of.
func stubDirectChannel(api *plugintest.API) {
	api.On("GetChannel", "user-dm").
		Return(&model.Channel{Id: "user-dm", Type: model.ChannelTypeDirect}, nil).Maybe()
	api.On("GetChannelMember", "user-dm", "bot-id").
		Return(&model.ChannelMember{}, nil).Maybe()
}
