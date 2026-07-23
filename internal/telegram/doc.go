// Package telegram implements ModemDeck's Telegram Bot API boundary and
// command domain without package-global runtime state.
//
// Config validation and Service.Run verify the bot identity before polling.
// Empty line scopes mean all lines; non-empty scopes constrain notifications,
// queries, SMS sends, and calls. Only the configured chat and administrator may
// execute commands. SMS and dial adapters must honor RequestID idempotently,
// and production checkpoints must be durable and scoped to one bot.
//
// Observer events are intentionally redacted. They never contain bot tokens,
// chat or administrator IDs, line IDs, phone numbers, SMS bodies, API
// descriptions, or raw dependency errors.
//
// This package does not manage HTTP proxies and does not implement Telegram
// voice calls.
package telegram
