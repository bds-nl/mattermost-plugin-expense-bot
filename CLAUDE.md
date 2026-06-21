# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Mattermost server plugin (`com.mattermost.plugin-expense-bot`) that lets users submit
expense claims by chatting with the **ExpenseBot** in a DM, then routes each claim to a
configured channel where it can be marked Paid or Rejected via message buttons. It was forked
from `mattermost-plugin-starter-template`; **there is no webapp** — all code is the Go server in
`server/` (package `main`). The README is the unmodified template README and describes generic
boilerplate, not this plugin.

## Commands

The Makefile drives everything. Go tools (`golangci-lint`, `gotestsum`) are auto-installed into
`./bin` by the targets that need them.

- `make` / `make dist` — lint, test, and build the plugin bundle into `dist/<id>.tar.gz`
  (cross-compiles server binaries for linux/darwin/windows).
- `make test` — run server tests via `gotestsum -- -v ./...`. Run a single test:
  `cd server && go test -run TestName ./...`.
- `make check-style` — run `golangci-lint` (config in `.golangci.yml`) plus a manifest consistency check.
- `make deploy` — build and upload to a running Mattermost server (needs local mode or
  `MM_ADMIN_*` / `MM_ADMIN_TOKEN` env vars; see README "Development").
- `make watch` — rebuild and redeploy the server binary on change during development.

When iterating on Go code locally, set `MM_SERVICESETTINGS_ENABLEDEVELOPER=1` so `make server`
builds only the host platform instead of all five targets.

## Architecture

The expense flow is a **conversational state machine** plus an **interactive-button HTTP API**,
all backed by the plugin KV store. There is no database.

- **`chat.go`** — the heart of the plugin. `MessageHasBeenPosted` fires on every post; it ignores
  anything that isn't a DM to the bot, then advances a per-user `Draft` through states
  (`DraftStateAsk*`: account → name → amount → description → file). User input is validated inline
  (IBAN via `go-iban`), each answer is persisted back to the draft, and the bot prompts for the
  next field. `expense`/`reset` are the magic words to start/abort. On completion it calls
  `createExpense` and saves the user's IBAN+name as `UserDefaults` so repeat users can skip those
  steps (`DraftStateAskDefaults`).
- **`expense.go`** — `createExpense` builds an `Expense`, posts a pinned DM summary to the
  submitter, persists it, and posts to the configured channel with **Paid / Reject** `SlackAttachment`
  buttons. `formatExpense` renders the markdown table used in both the DM and channel posts.
- **`api.go`** — `ServeHTTP` (gorilla/mux) exposes `POST /api/expenses/{id}/{state}`, the button
  target. `UpdateExpense` loads the expense, sets its state, and rewrites both the submitter's DM
  post and the channel post. All routes require the `Mattermost-User-ID` header.
- **`kvstore.go`** — the `KVStore` interface and its impl over `plugin.API` KV methods. Three
  key namespaces: `user:<id>` (UserDefaults), `draft:<id>` (in-progress Draft), `expense:<id>` (Expense).
- **`plugin.go`** — `OnActivate` ensures the `expensebot` bot account and wires up the KV store.
- **`configuration.go`** — single setting `ChannelID` (the posting channel), managed with the
  template's clone-under-lock pattern. Don't hand-edit `manifest.go` (generated) or `plugin.json`
  settings without keeping the two in sync — `make check-style` verifies this.

### Gotchas

- The plugin ID is read from `manifest.Id` (generated `manifest.go`, sourced from `plugin.json`).
  Use `manifest.Id` rather than hardcoding the ID — e.g. the button `Integration.URL` strings in
  `expense.go`.
- The Go module path is `github.com/bds-nl/mattermost-plugin-expense-bot` (go.mod), but
  `goimports.local-prefixes` in `.golangci.yml` may still reference the template default. Internal
  imports use package `main`, so the module name rarely matters.
- Tests live in `server/*_test.go` (package `main`). They use the `plugintest.API` mock
  (testify) for the Mattermost API; the conversational state machine is tested against an
  in-memory `fakeKVStore` injected into `Plugin.kvstore`. Shared test helpers
  (`newTestPlugin`, `fakeKVStore`, `stubDirectChannel`, `directMessagePost`) are in
  `server/main_test.go`. Coverage: `chat_test.go` (DM state machine), `expense_test.go`
  (`formatExpense`/`createExpense`), `api_test.go` (`UpdateExpense`, auth middleware),
  `kvstore_test.go` (KV round-trips).
