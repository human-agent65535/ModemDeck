package store

import (
	"context"
	"testing"
	"time"
)

func TestFavoritePreservesUnreadInNewThread(t *testing.T) {
	repository, database := newContactTestStore(t)
	ctx := context.Background()
	insertTestLine(t, database, "line_review", "+819011111111")
	user, err := repository.CreateMember(ctx, CreateMemberInput{Username: "review", PasswordHash: "hash", LineIDs: []string{"line_review"}})
	if err != nil {
		t.Fatal(err)
	}
	userCtx := memberTestContext(user)
	now := time.Now().UTC()
	_, _, err = repository.UpsertHardwareMessage(ctx, HardwareMessage{LineID: "line_review", EndpointLineID: "endpoint-review", EndpointMessageID: "sms-review", Number: "+819012345678", Text: "unread", Direction: "incoming", State: "received", Timestamp: now, ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repository.MessageUnreadSummary(userCtx)
	if err != nil {
		t.Fatal(err)
	}
	if before.UnreadMessageCount != 1 {
		t.Fatalf("setup unread = %+v", before)
	}
	err = repository.UpdateMessageThreads(userCtx, []MessageThreadIdentity{{LineID: "line_review", Peer: "+819012345678"}}, MessageThreadFavorite)
	if err != nil {
		t.Fatal(err)
	}
	after, err := repository.MessageUnreadSummary(userCtx)
	if err != nil {
		t.Fatal(err)
	}
	if after.UnreadMessageCount != before.UnreadMessageCount {
		t.Fatalf("favorite changed unread count: before=%+v after=%+v", before, after)
	}
}
