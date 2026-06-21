package main

import (
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validIBAN is a well-formed IBAN; its PrintCode form (grouped in fours) is what
// the plugin stores.
const validIBAN = "NL91ABNA0417164300"
const validIBANPrinted = "NL91 ABNA 0417 1643 00"

func lastMessage(t *testing.T, messages *[]string) string {
	t.Helper()
	require.NotEmpty(t, *messages, "expected at least one DM to be sent")
	return (*messages)[len(*messages)-1]
}

func TestNormalizeCmd(t *testing.T) {
	cases := map[string]string{
		"expense":     "expense",
		"  EXPENSE  ": "expense",
		"Reset":       "reset",
		"\tReSeT\n":   "reset",
		"hello world": "hello world",
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeCmd(in))
	}
}

func TestMessageIgnoredForOwnPosts(t *testing.T) {
	p, _, _, messages := newTestPlugin()
	p.MessageHasBeenPosted(nil, &model.Post{UserId: "bot-id", ChannelId: "user-dm"})
	assert.Empty(t, *messages, "bot must not react to its own posts")
}

func TestMessageIgnoredOutsideDirectChannel(t *testing.T) {
	p, api, _, messages := newTestPlugin()
	api.On("GetChannel", "open-channel").
		Return(&model.Channel{Id: "open-channel", Type: model.ChannelTypeOpen}, nil)

	p.MessageHasBeenPosted(nil, &model.Post{UserId: "u1", ChannelId: "open-channel"})
	assert.Empty(t, *messages, "non-DM posts must be ignored")
}

func TestUnknownMessageWithoutDraftGetsGreeting(t *testing.T) {
	p, api, _, messages := newTestPlugin()
	stubDirectChannel(api)

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "hi there"))
	assert.Contains(t, lastMessage(t, messages), "I'm ExpenseBot")
}

func TestExpenseCommandStartsDraftWithoutDefaults(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "expense"))

	draft := kv.drafts["u1"]
	require.NotNil(t, draft)
	assert.Equal(t, DraftStateAskAccount, draft.State)
	assert.Contains(t, lastMessage(t, messages), "IBAN")
}

func TestExpenseCommandWithDefaultsAsksToReuse(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.defaults["u1"] = &UserDefaults{UserID: "u1", Account: validIBANPrinted, Name: "Jane"}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "expense"))

	draft := kv.drafts["u1"]
	require.NotNil(t, draft)
	assert.Equal(t, DraftStateAskDefaults, draft.State)
	last := lastMessage(t, messages)
	assert.Contains(t, last, validIBANPrinted)
	assert.Contains(t, last, "Jane")
}

func TestResetDeletesDraft(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskName, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "reset"))

	_, exists := kv.drafts["u1"]
	assert.False(t, exists, "reset must delete the draft")
	assert.Contains(t, lastMessage(t, messages), "start a new expense")
}

func TestAskAccountRejectsInvalidIBAN(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskAccount, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "not-an-iban"))

	assert.Equal(t, DraftStateAskAccount, kv.drafts["u1"].State, "state must not advance on invalid input")
	assert.Contains(t, lastMessage(t, messages), "Invalid IBAN")
}

func TestAskAccountAcceptsValidIBAN(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskAccount, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", validIBAN))

	draft := kv.drafts["u1"]
	assert.Equal(t, DraftStateAskName, draft.State)
	assert.Equal(t, validIBANPrinted, draft.Data["iban"])
	assert.Contains(t, lastMessage(t, messages), "name")
}

func TestAskNameAdvancesToAmount(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskName, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "Jane Doe"))

	draft := kv.drafts["u1"]
	assert.Equal(t, DraftStateAskAmount, draft.State)
	assert.Equal(t, "Jane Doe", draft.Data["name"])
	assert.Contains(t, lastMessage(t, messages), "amount")
}

func TestAskFileRequiresAttachment(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskFile, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "here you go"))

	assert.Equal(t, DraftStateAskFile, kv.drafts["u1"].State, "state must not advance without a file")
	assert.Contains(t, lastMessage(t, messages), "at least one file")
}

func TestAskDefaultsYesReusesStoredValues(t *testing.T) {
	p, api, kv, _ := newTestPlugin()
	stubDirectChannel(api)
	kv.defaults["u1"] = &UserDefaults{UserID: "u1", Account: validIBANPrinted, Name: "Jane"}
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskDefaults, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "y"))

	draft := kv.drafts["u1"]
	assert.Equal(t, DraftStateAskAmount, draft.State)
	assert.Equal(t, validIBANPrinted, draft.Data["iban"])
	assert.Equal(t, "Jane", draft.Data["name"])
}

func TestAskDefaultsNoRestartsFromAccount(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.defaults["u1"] = &UserDefaults{UserID: "u1", Account: validIBANPrinted, Name: "Jane"}
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskDefaults, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "no"))

	assert.Equal(t, DraftStateAskAccount, kv.drafts["u1"].State)
	assert.Contains(t, lastMessage(t, messages), "IBAN")
}

func TestAskDefaultsRejectsUnrecognizedAnswer(t *testing.T) {
	p, api, kv, messages := newTestPlugin()
	stubDirectChannel(api)
	kv.defaults["u1"] = &UserDefaults{UserID: "u1", Account: validIBANPrinted, Name: "Jane"}
	kv.drafts["u1"] = &Draft{UserID: "u1", State: DraftStateAskDefaults, Data: map[string]string{}}

	p.MessageHasBeenPosted(nil, directMessagePost("u1", "maybe"))

	assert.Equal(t, DraftStateAskDefaults, kv.drafts["u1"].State, "state must not advance on an unclear answer")
	assert.Contains(t, strings.ToLower(lastMessage(t, messages)), "yes or no")
}
