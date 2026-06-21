package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newUpdateRequest(t *testing.T, expenseID, state string, body *model.PostActionIntegrationRequest) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/expenses/"+expenseID+"/"+state, bytes.NewReader(raw))
	return mux.SetURLVars(req, map[string]string{"id": expenseID, "state": state})
}

func TestUpdateExpenseSetsStateAndRewritesPosts(t *testing.T) {
	p, api, kv, _ := newTestPlugin()
	stubBaseURL(api, "https://chat.example.com")
	stubFileInfo(api, "file1", "receipt.pdf")

	kv.expenses["exp1"] = &Expense{
		ID:      "exp1",
		PostID:  "dm-post",
		UserID:  "u1",
		State:   ExpenseStateSubmitted,
		FileIDs: []string{"file1"},
	}

	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("LogInfo", mock.Anything).Maybe()
	api.On("GetPost", "dm-post").Return(&model.Post{Id: "dm-post"}, nil)
	api.On("GetUser", "u1").Return(&model.User{Id: "u1", FirstName: "Jane", LastName: "Doe"}, nil)
	api.On("UpdatePost", mock.AnythingOfType("*model.Post")).Return(&model.Post{}, nil)

	w := httptest.NewRecorder()
	req := newUpdateRequest(t, "exp1", ExpenseStatePaid, &model.PostActionIntegrationRequest{
		PostId:    "channel-post",
		ChannelId: "expense-channel",
	})

	p.UpdateExpense(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "OK", w.Body.String())
	assert.Equal(t, ExpenseStatePaid, kv.expenses["exp1"].State, "expense state should be persisted")
}

func TestUpdateExpenseRejectsInvalidBody(t *testing.T) {
	p, api, _, _ := newTestPlugin()
	api.On("LogWarn", mock.Anything).Maybe()

	req := httptest.NewRequest(http.MethodPost, "/api/expenses/exp1/Paid", bytes.NewReader([]byte("not json")))
	req = mux.SetURLVars(req, map[string]string{"id": "exp1", "state": ExpenseStatePaid})
	w := httptest.NewRecorder()

	p.UpdateExpense(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMattermostAuthorizationRequired(t *testing.T) {
	p, _, _, _ := newTestPlugin()

	called := false
	handler := p.MattermostAuthorizationRequired(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	t.Run("without user id", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/expenses/x/Paid", nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.False(t, called)
	})

	t.Run("with user id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/expenses/x/Paid", nil)
		req.Header.Set("Mattermost-User-ID", "u1")
		handler.ServeHTTP(w, req)
		assert.True(t, called)
	})
}
