package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

func TestMultiUserCommunicationAndPersonalStateIsolation(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	insertTestLine(t, database, "line_alpha", "+819011111111")
	insertTestLine(t, database, "line_beta", "+819022222222")

	alphaMessageID := insertTestMessage(
		t,
		database,
		"line_alpha",
		"+819012345678",
		"alpha history",
		"2026-07-29 08:00:00",
	)
	insertTestThread(
		t,
		database,
		"line_alpha",
		"+819012345678",
		alphaMessageID,
		"alpha history",
		"2026-07-29 08:00:00",
	)
	betaMessageID := insertTestMessage(
		t,
		database,
		"line_beta",
		"+819087654321",
		"beta history",
		"2026-07-29 08:01:00",
	)
	insertTestThread(
		t,
		database,
		"line_beta",
		"+819087654321",
		betaMessageID,
		"beta history",
		"2026-07-29 08:01:00",
	)
	insertTestCall(
		t,
		database,
		"call-alpha-history",
		"line_alpha",
		"+819012345678",
		"2026-07-29 08:02:00",
	)
	insertTestCall(
		t,
		database,
		"call-beta-history",
		"line_beta",
		"+819087654321",
		"2026-07-29 08:03:00",
	)
	insertTestRecording(t, database, "recording-alpha", "call-alpha-history")
	insertTestRecording(t, database, "recording-beta", "call-beta-history")

	alice, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "alice",
		PasswordHash: "alice-hash",
		LineIDs:      []string{"line_alpha"},
	})
	if err != nil {
		t.Fatalf("CreateMember(alice) error = %v", err)
	}
	bob, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "bob",
		PasswordHash: "bob-hash",
		LineIDs:      []string{"line_alpha", "line_beta"},
	})
	if err != nil {
		t.Fatalf("CreateMember(bob) error = %v", err)
	}

	aliceCtx := memberTestContext(alice)
	bobCtx := memberTestContext(bob)
	bobSettings, err := repository.LineSettings(bobCtx)
	if err != nil {
		t.Fatalf("LineSettings(bob before personal update) error = %v", err)
	}
	if _, err := repository.UpdateLineSettings(
		bobCtx,
		"line_beta",
		bobSettings.Revision,
	); err != nil {
		t.Fatalf("UpdateLineSettings(bob) error = %v", err)
	}
	if _, err := repository.CreateContact(aliceCtx, ContactInput{
		DisplayName:     "Hidden line",
		PreferredLineID: "line_beta",
		Phones: []ContactPhoneInput{{
			Number:  "+81 90 0000 0001",
			Primary: true,
		}},
	}); !errors.Is(err, ErrContactValidation) {
		t.Fatalf("CreateContact(unassigned preferred line) error = %v, want validation", err)
	}
	aliceContact, err := repository.CreateContact(aliceCtx, ContactInput{
		DisplayName: "Alice Address Book",
		Phones: []ContactPhoneInput{{
			Label:   "mobile",
			Number:  "+81 90 1234 5678",
			Primary: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContact(alice) error = %v", err)
	}
	bobContact, err := repository.CreateContact(bobCtx, ContactInput{
		DisplayName: "Bob Address Book",
		Phones: []ContactPhoneInput{{
			Label:   "mobile",
			Number:  "+81 90 1234 5678",
			Primary: true,
		}},
	})
	if err != nil {
		t.Fatalf("CreateContact(bob) error = %v", err)
	}

	assertContactName(t, repository, aliceCtx, "+819012345678", "Alice Address Book")
	assertContactName(t, repository, bobCtx, "+819012345678", "Bob Address Book")
	if err := repository.SetProfileContact(aliceCtx, aliceContact.ID); err != nil {
		t.Fatalf("SetProfileContact(own) error = %v", err)
	}
	if err := repository.SetProfileContact(aliceCtx, bobContact.ID); !errors.Is(err, ErrContactNotFound) {
		t.Fatalf("SetProfileContact(other user) error = %v, want contact not found", err)
	}

	aliceThreads, err := repository.MessageThreads(aliceCtx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads(alice) error = %v", err)
	}
	if len(aliceThreads) != 1 || aliceThreads[0].LineID != "line_alpha" {
		t.Fatalf("MessageThreads(alice) = %+v, want only line_alpha", aliceThreads)
	}
	if aliceThreads[0].UnreadCount != 0 {
		t.Fatalf("historical alice thread unread = %d, want 0", aliceThreads[0].UnreadCount)
	}
	bobThreads, err := repository.MessageThreads(bobCtx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads(bob) error = %v", err)
	}
	if len(bobThreads) != 2 {
		t.Fatalf("MessageThreads(bob) = %+v, want both assigned lines", bobThreads)
	}

	insertTestCall(
		t,
		database,
		"call-alpha-new",
		"line_alpha",
		"+819012345678",
		"2026-07-29 09:02:00",
	)
	newMessageID := insertTestMessage(
		t,
		database,
		"line_alpha",
		"+819012345678",
		"alpha new",
		"2026-07-29 09:00:00",
	)
	if _, err := database.Exec(`
		UPDATE sms_contacts
		SET last_sms_id = ?, last_timestamp = ?, last_content = ?, last_type = 1
		WHERE line_id = 'line_alpha' AND peer = '+819012345678'
	`, newMessageID, "2026-07-29 09:00:00", "alpha new"); err != nil {
		t.Fatalf("update alpha thread: %v", err)
	}
	if err := repository.UpdateMessageThreads(
		aliceCtx,
		[]MessageThreadIdentity{{LineID: "line_alpha", Peer: "+819012345678"}},
		MessageThreadMarkRead,
	); err != nil {
		t.Fatalf("mark alice thread read: %v", err)
	}
	if err := repository.UpdateMessageThreads(
		aliceCtx,
		[]MessageThreadIdentity{{LineID: "line_alpha", Peer: "+819012345678"}},
		MessageThreadFavorite,
	); err != nil {
		t.Fatalf("favorite alice thread: %v", err)
	}
	assertThreadState(t, repository, aliceCtx, 0, true)
	assertThreadState(t, repository, bobCtx, 1, false)

	if err := repository.MarkMissedCallsReadByIDs(aliceCtx, []string{"call-alpha-new"}); err != nil {
		t.Fatalf("mark alice call read: %v", err)
	}
	if err := repository.SetCallFavoritesByIDs(aliceCtx, []string{"call-alpha-new"}, true); err != nil {
		t.Fatalf("favorite alice call: %v", err)
	}
	assertCallState(t, repository, aliceCtx, "call-alpha-new", true, true)
	assertCallState(t, repository, bobCtx, "call-alpha-new", false, false)

	if err := repository.SetRecordingFavorites(
		aliceCtx,
		[]RecordingIdentity{{CallID: "call-alpha-history", ID: "recording-alpha"}},
		true,
	); err != nil {
		t.Fatalf("favorite alice recording: %v", err)
	}
	assertRecordingState(t, repository, aliceCtx, "recording-alpha", true)
	assertRecordingState(t, repository, bobCtx, "recording-alpha", false)

	aliceSettings, err := repository.LineSettings(aliceCtx)
	if err != nil {
		t.Fatalf("LineSettings(alice) error = %v", err)
	}
	bobSettings, err = repository.LineSettings(bobCtx)
	if err != nil {
		t.Fatalf("LineSettings(bob) error = %v", err)
	}
	if aliceSettings.DefaultLineID != "line_alpha" || bobSettings.DefaultLineID != "line_beta" {
		t.Fatalf(
			"personal default lines = %q / %q, want line_alpha / line_beta",
			aliceSettings.DefaultLineID,
			bobSettings.DefaultLineID,
		)
	}

	aliceLanguage, err := repository.SystemSettings(aliceCtx)
	if err != nil {
		t.Fatalf("SystemSettings(alice) error = %v", err)
	}
	aliceLanguage, err = repository.UpdateSystemSettings(
		aliceCtx,
		SystemLanguageEnUS,
		aliceLanguage.Revision,
	)
	if err != nil {
		t.Fatalf("UpdateSystemSettings(alice) error = %v", err)
	}
	bobLanguage, err := repository.SystemSettings(bobCtx)
	if err != nil {
		t.Fatalf("SystemSettings(bob) error = %v", err)
	}
	if aliceLanguage.Language != SystemLanguageEnUS ||
		bobLanguage.Language != SystemLanguageAuto {
		t.Fatalf(
			"personal languages = %q / %q, want en-US / auto",
			aliceLanguage.Language,
			bobLanguage.Language,
		)
	}

	aliceRecording, err := repository.RecordingSettings(aliceCtx)
	if err != nil {
		t.Fatalf("RecordingSettings(alice) error = %v", err)
	}
	aliceRecording, err = repository.UpdateRecordingSettings(
		aliceCtx,
		true,
		aliceRecording.Revision,
	)
	if err != nil {
		t.Fatalf("UpdateRecordingSettings(alice) error = %v", err)
	}
	bobRecording, err := repository.RecordingSettings(bobCtx)
	if err != nil {
		t.Fatalf("RecordingSettings(bob) error = %v", err)
	}
	if !aliceRecording.DefaultEnabled || bobRecording.DefaultEnabled {
		t.Fatalf(
			"personal recording defaults = %t / %t, want true / false",
			aliceRecording.DefaultEnabled,
			bobRecording.DefaultEnabled,
		)
	}
}

func TestMessageReadWatermarkUsesIngestionOrder(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	const (
		lineID = "line_read_watermark"
		peer   = "+819055501234"
	)
	insertTestLine(t, database, lineID, "+819055500000")
	displayedMessageID := insertTestMessage(
		t,
		database,
		lineID,
		peer,
		"displayed latest",
		"2026-07-30 10:00:00",
	)
	insertTestThread(
		t,
		database,
		lineID,
		peer,
		displayedMessageID,
		"displayed latest",
		"2026-07-30 10:00:00",
	)
	historicalBackfillID := insertTestMessage(
		t,
		database,
		lineID,
		peer,
		"historical backfill before assignment",
		"2026-07-30 08:00:00",
	)
	if _, err := database.Exec(`
		UPDATE sms_contacts
		SET unread_count = unread_count + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE line_id = ? AND peer = ?
	`, lineID, peer); err != nil {
		t.Fatalf("record historical backfill: %v", err)
	}

	user, err := repository.CreateMember(ctx, CreateMemberInput{
		Username:     "watermark-reader",
		PasswordHash: "watermark-reader-hash",
		LineIDs:      []string{lineID},
	})
	if err != nil {
		t.Fatalf("CreateMember() error = %v", err)
	}
	userCtx := memberTestContext(user)
	threads, err := repository.MessageThreads(userCtx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() after assignment error = %v", err)
	}
	if len(threads) != 1 ||
		threads[0].LastMessageID != displayedMessageID ||
		threads[0].UnreadCount != 0 {
		t.Fatalf(
			"assigned historical thread = %+v, want displayed ID %d and no unread",
			threads,
			displayedMessageID,
		)
	}

	backfilledMessageID := insertTestMessage(
		t,
		database,
		lineID,
		peer,
		"arrived later with an older timestamp",
		"2026-07-30 09:00:00",
	)
	if backfilledMessageID <= historicalBackfillID {
		t.Fatalf(
			"backfilled message ID = %d, want greater than historical ID %d",
			backfilledMessageID,
			historicalBackfillID,
		)
	}
	if _, err := database.Exec(`
		UPDATE sms_contacts
		SET unread_count = unread_count + 1,
			updated_at = CURRENT_TIMESTAMP
		WHERE line_id = ? AND peer = ?
	`, lineID, peer); err != nil {
		t.Fatalf("record backfilled unread message: %v", err)
	}

	threads, err = repository.MessageThreads(userCtx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() before read error = %v", err)
	}
	if len(threads) != 1 ||
		threads[0].LastMessageID != displayedMessageID ||
		threads[0].UnreadCount != 1 {
		t.Fatalf(
			"thread before read = %+v, want displayed ID %d and one unread",
			threads,
			displayedMessageID,
		)
	}

	identity := MessageThreadIdentity{LineID: lineID, Peer: peer}
	if err := repository.UpdateMessageThreads(
		userCtx,
		[]MessageThreadIdentity{identity},
		MessageThreadMarkRead,
	); err != nil {
		t.Fatalf("mark backfilled thread read: %v", err)
	}
	threads, err = repository.MessageThreads(userCtx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() after read error = %v", err)
	}
	if len(threads) != 1 || threads[0].UnreadCount != 0 {
		t.Fatalf("thread remains unread after read: %+v", threads)
	}

	var readThrough int64
	if err := database.QueryRow(`
		SELECT last_read_sms_id
		FROM modemdeck_user_message_thread_state
		WHERE user_id = ? AND line_id = ? AND peer = ?
	`, user.ID, lineID, peer).Scan(&readThrough); err != nil {
		t.Fatalf("read message watermark: %v", err)
	}
	if readThrough != backfilledMessageID {
		t.Fatalf(
			"read watermark = %d, want ingestion boundary %d",
			readThrough,
			backfilledMessageID,
		)
	}

	const futureWatermark = int64(10_000)
	if _, err := database.Exec(`
		UPDATE modemdeck_user_message_thread_state
		SET last_read_sms_id = ?, marked_unread = 1
		WHERE user_id = ? AND line_id = ? AND peer = ?
	`, futureWatermark, user.ID, lineID, peer); err != nil {
		t.Fatalf("seed future read watermark: %v", err)
	}
	if err := repository.UpdateMessageThreads(
		userCtx,
		[]MessageThreadIdentity{identity},
		MessageThreadMarkRead,
	); err != nil {
		t.Fatalf("repeat mark read: %v", err)
	}
	var markedUnread bool
	if err := database.QueryRow(`
		SELECT last_read_sms_id, marked_unread
		FROM modemdeck_user_message_thread_state
		WHERE user_id = ? AND line_id = ? AND peer = ?
	`, user.ID, lineID, peer).Scan(&readThrough, &markedUnread); err != nil {
		t.Fatalf("read repeated message watermark: %v", err)
	}
	if readThrough != futureWatermark || markedUnread {
		t.Fatalf(
			"repeated read state = watermark %d marked %t, want %d / false",
			readThrough,
			markedUnread,
			futureWatermark,
		)
	}
}

func memberTestContext(user User) context.Context {
	return auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID:         user.ID,
		Username:       user.Username,
		Role:           auth.RoleMember,
		AllowedLineIDs: append([]string(nil), user.LineIDs...),
	})
}

func insertTestLine(t *testing.T, database *sql.DB, lineID, phoneNumber string) {
	t.Helper()
	if _, err := database.Exec(
		`INSERT INTO modemdeck_lines (line_id, phone_number, line_label)
		 VALUES (?, ?, ?)`,
		lineID,
		phoneNumber,
		lineID,
	); err != nil {
		t.Fatalf("insert line %s: %v", lineID, err)
	}
}

func insertTestMessage(
	t *testing.T,
	database *sql.DB,
	lineID, peer, content, timestamp string,
) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO sms (
			line_id, peer, sender, recipient, content, type, timestamp, created_at
		) VALUES (?, ?, ?, '', ?, 1, ?, ?)
	`, lineID, peer, peer, content, timestamp, timestamp)
	if err != nil {
		t.Fatalf("insert message for %s: %v", lineID, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read inserted message id: %v", err)
	}
	return id
}

func insertTestThread(
	t *testing.T,
	database *sql.DB,
	lineID, peer string,
	messageID int64,
	content, timestamp string,
) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO sms_contacts (
			line_id, imsi, peer, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, created_at, updated_at
		) VALUES (?, '', ?, ?, ?, ?, 1, 1, ?, ?)
	`, lineID, peer, messageID, timestamp, content, timestamp, timestamp); err != nil {
		t.Fatalf("insert message thread for %s: %v", lineID, err)
	}
}

func insertTestCall(
	t *testing.T,
	database *sql.DB,
	callID, lineID, remoteNumber, timestamp string,
) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO call_history (
			id, line_id, direction, remote_number, phase,
			created_at, updated_at, ended_at
		) VALUES (?, ?, 'incoming', ?, 'ended', ?, ?, ?)
	`, callID, lineID, remoteNumber, timestamp, timestamp, timestamp); err != nil {
		t.Fatalf("insert call %s: %v", callID, err)
	}
}

func insertTestRecording(t *testing.T, database *sql.DB, recordingID, callID string) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO modemdeck_call_recordings (
			id, call_id, segment_index, status, started_at, ended_at,
			duration_ms, size_bytes, relative_path
		) VALUES (?, ?, 1, 'ready', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
			1000, 128, ?)
	`, recordingID, callID, recordingID+".ogg"); err != nil {
		t.Fatalf("insert recording %s: %v", recordingID, err)
	}
}

func assertContactName(
	t *testing.T,
	repository *Store,
	ctx context.Context,
	number, want string,
) {
	t.Helper()
	name, err := repository.ContactNameForNumber(ctx, number)
	if err != nil {
		t.Fatalf("ContactNameForNumber(%s) error = %v", number, err)
	}
	if name != want {
		t.Fatalf("ContactNameForNumber(%s) = %q, want %q", number, name, want)
	}
}

func assertThreadState(
	t *testing.T,
	repository *Store,
	ctx context.Context,
	wantUnread int64,
	wantFavorite bool,
) {
	t.Helper()
	threads, err := repository.MessageThreads(ctx, ThreadQuery{})
	if err != nil {
		t.Fatalf("MessageThreads() error = %v", err)
	}
	for _, thread := range threads {
		if thread.LineID != "line_alpha" || thread.Peer != "+819012345678" {
			continue
		}
		if thread.UnreadCount != wantUnread || thread.Favorite != wantFavorite {
			t.Fatalf(
				"alpha thread state = unread %d favorite %t, want %d / %t",
				thread.UnreadCount,
				thread.Favorite,
				wantUnread,
				wantFavorite,
			)
		}
		return
	}
	t.Fatal("line_alpha thread not found")
}

func assertCallState(
	t *testing.T,
	repository *Store,
	ctx context.Context,
	callID string,
	wantRead, wantFavorite bool,
) {
	t.Helper()
	calls, err := repository.Calls(ctx, CallQuery{})
	if err != nil {
		t.Fatalf("Calls() error = %v", err)
	}
	for _, call := range calls {
		if call.ID != callID {
			continue
		}
		if call.Read != wantRead || call.Favorite != wantFavorite {
			t.Fatalf(
				"call %s state = read %t favorite %t, want %t / %t",
				callID,
				call.Read,
				call.Favorite,
				wantRead,
				wantFavorite,
			)
		}
		return
	}
	t.Fatalf("call %s not found", callID)
}

func assertRecordingState(
	t *testing.T,
	repository *Store,
	ctx context.Context,
	recordingID string,
	wantFavorite bool,
) {
	t.Helper()
	entries, err := repository.RecordingEntries(ctx, RecordingQuery{})
	if err != nil {
		t.Fatalf("RecordingEntries() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Segment.ID != recordingID {
			continue
		}
		if entry.Favorite != wantFavorite {
			t.Fatalf(
				"recording %s favorite = %t, want %t",
				recordingID,
				entry.Favorite,
				wantFavorite,
			)
		}
		return
	}
	t.Fatalf("recording %s not found", recordingID)
}
