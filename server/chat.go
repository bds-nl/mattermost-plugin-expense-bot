package main

import (
	"fmt"
	"strings"

	"github.com/almerlucke/go-iban/iban"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	DraftStateAskName        = "ask_name"
	DraftStateAskAccount     = "ask_account"
	DraftStateAskAmount      = "ask_amount"
	DraftStateAskDescription = "ask_description"
	DraftStateAskFile        = "ask_file"
	DraftStateAskDefaults    = "ask_defaults"
	ExpenseStateSubmitted    = "Submitted"
	ExpenseStatePaid         = "Paid"
	ExpenseStateRejected     = "Rejected"
)

func (p *Plugin) MessageHasBeenPosted(c *plugin.Context, post *model.Post) {
	if post.UserId == p.botID {
		return // bot own messages
	}
	channel, _ := p.API.GetChannel(post.ChannelId)
	if channel.Type != model.ChannelTypeDirect {
		return
	}
	if member, appErr := p.API.GetChannelMember(channel.Id, p.botID); appErr != nil || member == nil {
		return
	}

	draft, err := p.kvstore.GetDraft(post.UserId)
	if err != nil {
		p.API.LogError("failed to get draft", "err", err.Error())
	}
	msg := post.Message

	if draft == nil {
		if normalizeCmd(msg) == "expense" {
			_ = p.sendDM(post.UserId, "Let's start the expense, shall we? If you change your mind, type ```reset``` and it will all be over.")
			var userDefaults *UserDefaults
			userDefaults, err = p.kvstore.GetUserDefaults(post.UserId)
			if err != nil {
				p.API.LogError("failed to get user defaults", "err", err.Error())
			}
			if userDefaults != nil {
				draft = &Draft{
					UserID: post.UserId,
					State:  DraftStateAskDefaults,
					Data:   map[string]string{},
				}
				err = p.kvstore.SaveDraft(post.UserId, draft)
				if err != nil {
					_ = p.sendDM(post.UserId, "System error, please try again")
				}
				_ = p.sendDM(post.UserId, fmt.Sprintf("Last time you used account **%s** and name **%s**. Do you want to use them again? (y[es]/n[o])", userDefaults.Account, userDefaults.Name))
				return
			}
			draft = &Draft{
				UserID: post.UserId,
				State:  DraftStateAskAccount,
				Data:   map[string]string{},
			}
			err = p.kvstore.SaveDraft(post.UserId, draft)
			if err != nil {
				_ = p.sendDM(post.UserId, "System error, please try again")
			}
			_ = p.sendDM(post.UserId, "**What is your IBAN?**")
			return
		}
		_ = p.sendDM(post.UserId, "Hi! I'm ExpenseBot, I'll help you submit an expense. Type ```expense``` to start a new expense.")
		return
	} else if normalizeCmd(msg) == "reset" {
		if err = p.kvstore.DeleteDraft(post.UserId); err != nil {
			p.API.LogError("failed to delete draft", "err", err.Error())
		}
		_ = p.sendDM(post.UserId, "Type ```expense``` to start a new expense.")
		return
	}
	switch draft.State {
	case DraftStateAskAccount:
		var account *iban.IBAN
		account, err = iban.NewIBAN(msg)
		if err != nil {
			_ = p.sendDM(post.UserId, "Invalid IBAN. Please try again.")
			return
		}
		draft.Data["iban"] = account.PrintCode
		draft.State = DraftStateAskName
		if err = p.kvstore.SaveDraft(post.UserId, draft); err != nil {
			p.API.LogError("failed to save draft", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		_ = p.sendDM(post.UserId, "**In what name is the account held?**")

	case DraftStateAskName:
		draft.Data["name"] = msg
		draft.State = DraftStateAskAmount
		if err = p.kvstore.SaveDraft(post.UserId, draft); err != nil {
			p.API.LogError("failed to save draft", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		_ = p.sendDM(post.UserId, "**What is the amount of the expense?** (e.g. 100.00)\n\nIf you combine multiple receipts, fill in the total amount.")

	case DraftStateAskAmount:
		draft.Data["amount"] = msg
		draft.State = DraftStateAskDescription
		if err = p.kvstore.SaveDraft(post.UserId, draft); err != nil {
			p.API.LogError("failed to save draft", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		_ = p.sendDM(post.UserId, "**In a few words, describe the expense.**")

	case DraftStateAskDescription:
		draft.Data["description"] = msg
		draft.State = DraftStateAskFile
		if err = p.kvstore.SaveDraft(post.UserId, draft); err != nil {
			p.API.LogError("failed to save draft", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		_ = p.sendDM(post.UserId, "**Upload the invoice or a picture of the receipt.**\n\nYou can drag 'n' drop a file into the chat window, or use the paperclip in the bottom right corner.\n\nIf you have multiple receipts, attach them all to a single message.")

	case DraftStateAskFile:
		if len(post.FileIds) == 0 {
			_ = p.sendDM(post.UserId, "Submit at least one file.")
			return
		}
		draft.Data["file"] = strings.Join(post.FileIds, ",")
		if err = p.createExpense(post.UserId, draft); err != nil {
			p.API.LogError("failed to create expense", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		if err = p.kvstore.SaveUserDefaults(&UserDefaults{
			UserID:  post.UserId,
			Account: draft.Data["iban"],
			Name:    draft.Data["name"],
		}); err != nil {
			p.API.LogError("failed to save user defaults", "err", err.Error())
		}
		if err = p.kvstore.DeleteDraft(post.UserId); err != nil {
			p.API.LogError("failed to delete draft", "err", err.Error())
		}
		_ = p.sendDM(post.UserId, "**Expense saved! :tada:**")
		_ = p.sendDM(post.UserId, "Type ```expense``` to start a new expense")

	case DraftStateAskDefaults:
		draft, err = p.kvstore.GetDraft(post.UserId)
		if err != nil {
			p.API.LogError("failed to get draft", "err", err.Error())
			_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			return
		}
		switch strings.ToLower(msg) {
		case "y", "yes":
			var userDefaults *UserDefaults
			userDefaults, err = p.kvstore.GetUserDefaults(post.UserId)
			if err != nil {
				p.API.LogError("failed to get user defaults", "err", err.Error())
				_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
				return
			}
			draft.Data["iban"] = userDefaults.Account
			draft.Data["name"] = userDefaults.Name
			draft.State = DraftStateAskAmount
			if err = p.kvstore.SaveDraft(post.UserId, draft); err != nil {
				p.API.LogError("failed to save draft", "err", err.Error())
				_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
				return
			}
			_ = p.sendDM(post.UserId, "Amazing, look at us being efficient! I will fill that in for you, let's start with the amount.")
			_ = p.sendDM(post.UserId, "**What is the amount of the expense?** (e.g. 100.00)\n\nIf you combine multiple receipts, fill in the total amount.")
		case "n", "no":
			draft.State = DraftStateAskAccount
			err = p.kvstore.SaveDraft(post.UserId, draft)
			if err != nil {
				p.API.LogError("failed to save draft", "err", err.Error())
				_ = p.sendDM(post.UserId, "System error, please try again or type ```reset``` to stop the expense.")
			}
			_ = p.sendDM(post.UserId, "No problem, let's start from the beginning.")
			_ = p.sendDM(post.UserId, "**What is your IBAN?**")
		default:
			_ = p.sendDM(post.UserId, "Please answer with yes or no. Just the first letter is enough.")
		}
	}
}

func (p *Plugin) sendDM(userID string, message string) *model.Post {
	return p.sendPinnedDM(userID, message, false)
}

func (p *Plugin) sendPinnedDM(userID string, message string, isPinned bool) *model.Post {
	channel, err := p.API.GetDirectChannel(p.botID, userID)
	if err != nil {
		p.API.LogError("failed to get direct channel", "err", err.Error())
		return nil
	}
	post, err := p.API.CreatePost(&model.Post{
		UserId:    p.botID,
		ChannelId: channel.Id,
		Message:   message,
		IsPinned:  isPinned,
	})
	if err != nil {
		p.API.LogError("failed to create post", "err", err.Error())
		return nil
	}
	return post
}

func normalizeCmd(cmd string) string {
	return strings.ToLower(strings.TrimSpace(cmd))
}
