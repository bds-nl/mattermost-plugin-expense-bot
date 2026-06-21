package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/pkg/errors"
)

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	router := mux.NewRouter()

	// Middleware to require that the user is logged in
	router.Use(p.MattermostAuthorizationRequired)

	apiRouter := router.PathPrefix("/api/").Subrouter()

	apiRouter.HandleFunc("/expenses/{id}/{state}", p.safeHandler(p.UpdateExpense)).Methods(http.MethodPost)

	router.ServeHTTP(w, r)
}

func (p *Plugin) MattermostAuthorizationRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (p *Plugin) safeHandler(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				p.API.LogError("panic in handler", "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		fn(w, r)
	}
}

func (p *Plugin) UpdateExpense(w http.ResponseWriter, r *http.Request) {
	var request *model.PostActionIntegrationRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&request)
	if decodeErr != nil || request == nil {
		p.API.LogWarn("failed to decode PostActionIntegrationRequest")
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	vars := mux.Vars(r)
	expenseID := vars["id"]
	state := vars["state"]

	p.API.LogInfo("Updating expense", "id", expenseID, "state", state)
	p.API.LogInfo(fmt.Sprintf("post_id: %s channel_id: %s", request.PostId, request.ChannelId))

	expense, err := p.kvstore.GetExpense(expenseID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	expense.State = state
	if err = p.kvstore.SaveExpense(expense); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if err = p.updateUser(expense); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if err = p.updateChannel(request, expense); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if _, err = w.Write([]byte("OK")); err != nil {
		p.API.LogError("Failed to write response", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (p *Plugin) updateUser(expense *Expense) error {
	post, appErr := p.API.GetPost(expense.PostID)
	if appErr != nil || post == nil {
		return errors.Wrap(appErr, "failed to get post")
	}
	message, err := p.formatExpense(expense)
	if err != nil {
		return errors.Wrap(err, "failed to format expense")
	}
	post.Message = message
	_, appErr = p.API.UpdatePost(post)
	if appErr != nil {
		return errors.Wrap(appErr, "failed to update post")
	}
	return nil
}

func (p *Plugin) updateChannel(request *model.PostActionIntegrationRequest, expense *Expense) error {
	user, appError := p.API.GetUser(expense.UserID)
	if appError != nil {
		return errors.Wrap(appError, "failed to get user")
	}
	message, err := p.formatExpense(expense)
	if err != nil {
		return errors.Wrap(err, "failed to format expense")
	}

	post := &model.Post{
		Id:        request.PostId,
		UserId:    p.botID,
		ChannelId: request.ChannelId,
		Message:   fmt.Sprintf("**Expense claim from %s**\n\n%s", user.Username, message),
		FileIds:   expense.FileIDs,
	}
	if _, appError = p.API.UpdatePost(post); appError != nil {
		return errors.Wrap(appError, "failed to update post")
	}
	return nil
}
