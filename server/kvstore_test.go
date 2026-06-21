package main

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// kvBackedStore returns a Store whose KVGet/KVSet/KVDelete are backed by an
// in-memory map, so we exercise the real marshal/unmarshal round-trip.
func kvBackedStore() (KVStore, map[string][]byte) {
	api := &plugintest.API{}
	data := map[string][]byte{}

	api.On("KVGet", mock.AnythingOfType("string")).
		Return(func(key string) []byte { return data[key] }, nil)
	api.On("KVSet", mock.AnythingOfType("string"), mock.AnythingOfType("[]uint8")).
		Return(func(key string, value []byte) *model.AppError {
			data[key] = value
			return nil
		})
	api.On("KVDelete", mock.AnythingOfType("string")).
		Return(func(key string) *model.AppError {
			delete(data, key)
			return nil
		})

	return NewKVStore(api), data
}

func TestDraftRoundTrip(t *testing.T) {
	store, data := kvBackedStore()

	draft := &Draft{
		UserID: "u1",
		State:  DraftStateAskAmount,
		Data:   map[string]string{"iban": validIBANPrinted, "name": "Jane"},
	}
	require.NoError(t, store.SaveDraft("u1", draft))
	assert.Contains(t, data, "draft:u1", "draft stored under namespaced key")

	got, err := store.GetDraft("u1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, draft.State, got.State)
	assert.Equal(t, draft.Data, got.Data)
}

func TestGetDraftMissingReturnsNil(t *testing.T) {
	store, _ := kvBackedStore()

	got, err := store.GetDraft("nobody")
	require.NoError(t, err)
	assert.Nil(t, got, "absent draft should be nil, not an error")
}

func TestDeleteDraft(t *testing.T) {
	store, data := kvBackedStore()
	require.NoError(t, store.SaveDraft("u1", &Draft{UserID: "u1", Data: map[string]string{}}))

	require.NoError(t, store.DeleteDraft("u1"))
	assert.NotContains(t, data, "draft:u1")
}

func TestUserDefaultsRoundTrip(t *testing.T) {
	store, data := kvBackedStore()

	require.NoError(t, store.SaveUserDefaults(&UserDefaults{UserID: "u1", Account: validIBANPrinted, Name: "Jane"}))
	assert.Contains(t, data, "user:u1")

	got, err := store.GetUserDefaults("u1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, validIBANPrinted, got.Account)
	assert.Equal(t, "Jane", got.Name)
}

func TestExpenseRoundTrip(t *testing.T) {
	store, data := kvBackedStore()

	expense := &Expense{ID: "exp1", UserID: "u1", State: ExpenseStateSubmitted, FileIDs: []string{"f1", "f2"}}
	require.NoError(t, store.SaveExpense(expense))
	assert.Contains(t, data, "expense:exp1")

	got, err := store.GetExpense("exp1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, expense.FileIDs, got.FileIDs)
	assert.Equal(t, ExpenseStateSubmitted, got.State)
}
