package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/phone"
)

func establishStableLineHomeCountry(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	observedRegion,
	canonicalPhone string,
) (string, bool, error) {
	lineID = strings.TrimSpace(lineID)
	observedRegion = phone.CanonicalRegion(observedRegion)
	if lineID == "" {
		return "", false, nil
	}
	storedRegion, err := stableLineHomeCountry(ctx, transaction, lineID)
	if err != nil {
		return "", false, err
	}
	if storedRegion != "" {
		return storedRegion, false, nil
	}
	if observedRegion == "" {
		if address, parseErr := phone.ParseSubscriber(canonicalPhone, ""); parseErr == nil {
			observedRegion = address.Region
		}
	}
	if observedRegion == "" {
		return "", false, nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`UPDATE modemdeck_lines
		 SET home_country_iso = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE line_id = ? AND home_country_iso = ''`,
		observedRegion,
		lineID,
	); err != nil {
		return "", false, fmt.Errorf("save stable line home country: %w", err)
	}
	return observedRegion, true, nil
}

func stableLineHomeCountry(
	ctx context.Context,
	queryer stableLineQueryer,
	lineID string,
) (string, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return "", nil
	}
	var region string
	if err := queryer.QueryRowContext(
		ctx,
		`SELECT home_country_iso FROM modemdeck_lines WHERE line_id = ?`,
		lineID,
	).Scan(&region); err != nil {
		return "", fmt.Errorf("read stable line home country: %w", err)
	}
	return phone.CanonicalRegion(region), nil
}

func canonicalizeLinePhoneIdentities(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	region string,
) error {
	lineID = strings.TrimSpace(lineID)
	region = strings.ToUpper(strings.TrimSpace(region))
	if lineID == "" || region == "" {
		return nil
	}
	if err := canonicalizeLineCallPhones(ctx, transaction, lineID, region); err != nil {
		return err
	}
	if err := canonicalizeLineMessagePhones(ctx, transaction, lineID, region); err != nil {
		return err
	}
	return canonicalizeLineMessageThreads(ctx, transaction, lineID, region)
}

func canonicalizeLineCallPhones(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	region string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT id, remote_number, reported_remote_number, local_phone
		 FROM call_history
		 WHERE line_id = ?`,
		lineID,
	)
	if err != nil {
		return fmt.Errorf("read line call phone identities: %w", err)
	}
	type update struct {
		id       string
		remote   string
		reported string
		local    string
	}
	updates := make([]update, 0)
	for rows.Next() {
		var id, remote, reported, local string
		if err := rows.Scan(&id, &remote, &reported, &local); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan line call phone identity: %w", err)
		}
		nextReported := strings.TrimSpace(reported)
		if nextReported == "" {
			nextReported = strings.TrimSpace(remote)
		}
		nextRemote := canonicalSubscriberOrOriginal(remote, region)
		nextLocal := canonicalSubscriberOrOriginal(local, region)
		if nextRemote == remote && nextReported == reported && nextLocal == local {
			continue
		}
		updates = append(updates, update{
			id:       id,
			remote:   nextRemote,
			reported: nextReported,
			local:    nextLocal,
		})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close line call phone identities: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read line call phone identities: %w", err)
	}
	for _, update := range updates {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE call_history
			 SET remote_number = ?, reported_remote_number = ?, local_phone = ?
			 WHERE id = ?`,
			update.remote,
			update.reported,
			update.local,
			update.id,
		); err != nil {
			return fmt.Errorf("update line call phone identity: %w", err)
		}
	}
	return nil
}

func canonicalizeLineMessagePhones(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	region string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT id, peer, reported_peer, local_phone, sender, recipient, type
		 FROM sms
		 WHERE line_id = ?`,
		lineID,
	)
	if err != nil {
		return fmt.Errorf("read line message phone identities: %w", err)
	}
	type update struct {
		id        int64
		peer      string
		reported  string
		local     string
		sender    string
		recipient string
	}
	updates := make([]update, 0)
	for rows.Next() {
		var (
			id                                       int64
			peer, reported, local, sender, recipient string
			messageType                              int64
		)
		if err := rows.Scan(
			&id,
			&peer,
			&reported,
			&local,
			&sender,
			&recipient,
			&messageType,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan line message phone identity: %w", err)
		}
		nextReported := strings.TrimSpace(reported)
		if nextReported == "" {
			nextReported = strings.TrimSpace(peer)
		}
		nextPeer := canonicalSubscriberOrOriginal(peer, region)
		nextLocal := canonicalSubscriberOrOriginal(local, region)
		nextSender, nextRecipient := sender, recipient
		switch messageType {
		case 1:
			nextSender, nextRecipient = nextPeer, nextLocal
		case 2:
			nextSender, nextRecipient = nextLocal, nextPeer
		}
		if nextPeer == peer &&
			nextReported == reported &&
			nextLocal == local &&
			nextSender == sender &&
			nextRecipient == recipient {
			continue
		}
		updates = append(updates, update{
			id:        id,
			peer:      nextPeer,
			reported:  nextReported,
			local:     nextLocal,
			sender:    nextSender,
			recipient: nextRecipient,
		})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close line message phone identities: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read line message phone identities: %w", err)
	}
	for _, update := range updates {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE sms
			 SET peer = ?, reported_peer = ?, local_phone = ?,
				sender = ?, recipient = ?
			 WHERE id = ?`,
			update.peer,
			update.reported,
			update.local,
			update.sender,
			update.recipient,
			update.id,
		); err != nil {
			return fmt.Errorf("update line message phone identity: %w", err)
		}
	}
	return nil
}

type canonicalMessageThread struct {
	imsi          string
	iccid         string
	peer          string
	lastSMSID     int64
	lastTimestamp sql.NullString
	lastContent   string
	lastType      int64
	unreadCount   int64
	markedUnread  bool
	favorite      bool
	createdAt     sql.NullString
	updatedAt     sql.NullString
}

func canonicalizeLineMessageThreads(
	ctx context.Context,
	transaction *sql.Tx,
	lineID,
	region string,
) error {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT imsi, iccid, peer, last_sms_id, last_timestamp,
			last_content, last_type, unread_count, marked_unread, is_favorite,
			created_at, updated_at
		 FROM sms_contacts
		 WHERE line_id = ?`,
		lineID,
	)
	if err != nil {
		return fmt.Errorf("read line message thread identities: %w", err)
	}
	merged := make(map[string]canonicalMessageThread)
	changed := false
	for rows.Next() {
		var thread canonicalMessageThread
		if err := rows.Scan(
			&thread.imsi,
			&thread.iccid,
			&thread.peer,
			&thread.lastSMSID,
			&thread.lastTimestamp,
			&thread.lastContent,
			&thread.lastType,
			&thread.unreadCount,
			&thread.markedUnread,
			&thread.favorite,
			&thread.createdAt,
			&thread.updatedAt,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan line message thread identity: %w", err)
		}
		canonicalPeer := canonicalSubscriberOrOriginal(thread.peer, region)
		if canonicalPeer != thread.peer {
			changed = true
		}
		thread.peer = canonicalPeer
		if existing, exists := merged[canonicalPeer]; exists {
			changed = true
			merged[canonicalPeer] = mergeCanonicalMessageThreads(existing, thread)
		} else {
			merged[canonicalPeer] = thread
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close line message thread identities: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read line message thread identities: %w", err)
	}
	if !changed {
		return nil
	}
	if _, err := transaction.ExecContext(
		ctx,
		`DELETE FROM sms_contacts WHERE line_id = ?`,
		lineID,
	); err != nil {
		return fmt.Errorf("replace line message thread identities: %w", err)
	}
	peers := make([]string, 0, len(merged))
	for peer := range merged {
		peers = append(peers, peer)
	}
	sort.Strings(peers)
	for _, peer := range peers {
		thread := merged[peer]
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO sms_contacts (
				line_id, imsi, iccid, peer, last_sms_id, last_timestamp,
				last_content, last_type, unread_count, marked_unread, is_favorite,
				created_at, updated_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			lineID,
			thread.imsi,
			thread.iccid,
			thread.peer,
			thread.lastSMSID,
			thread.lastTimestamp,
			thread.lastContent,
			thread.lastType,
			thread.unreadCount,
			thread.markedUnread,
			thread.favorite,
			thread.createdAt,
			thread.updatedAt,
		); err != nil {
			return fmt.Errorf("insert canonical line message thread: %w", err)
		}
	}
	return nil
}

func mergeCanonicalMessageThreads(
	left,
	right canonicalMessageThread,
) canonicalMessageThread {
	latest, older := left, right
	if canonicalMessageThreadIsNewer(right, left) {
		latest, older = right, left
	}
	latest.unreadCount += older.unreadCount
	latest.markedUnread = latest.markedUnread || older.markedUnread
	latest.favorite = latest.favorite || older.favorite
	latest.createdAt = earliestNullableTime(left.createdAt, right.createdAt)
	latest.updatedAt = latestNullableTime(left.updatedAt, right.updatedAt)
	return latest
}

func canonicalMessageThreadIsNewer(left, right canonicalMessageThread) bool {
	if left.lastTimestamp.String != right.lastTimestamp.String {
		return left.lastTimestamp.String > right.lastTimestamp.String
	}
	return left.lastSMSID > right.lastSMSID
}

func earliestNullableTime(left, right sql.NullString) sql.NullString {
	switch {
	case !left.Valid:
		return right
	case !right.Valid:
		return left
	case left.String <= right.String:
		return left
	default:
		return right
	}
}

func latestNullableTime(left, right sql.NullString) sql.NullString {
	switch {
	case !left.Valid:
		return right
	case !right.Valid:
		return left
	case left.String >= right.String:
		return left
	default:
		return right
	}
}

func canonicalSubscriberOrOriginal(value, region string) string {
	value = strings.TrimSpace(value)
	if canonical := phone.NetworkSubscriberE164(value, region); canonical != "" {
		return canonical
	}
	return value
}
