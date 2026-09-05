package httpapi

import (
	"bytes"
	"context"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestAccessReductionClosesExistingMedia(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		wantRevoked bool
	}{
		{"disable pairing", `{"username":"alice","enabled":true,"ios_pairing_enabled":false,"line_ids":["line-1"],"revision":1}`, true},
		{"remove line", `{"username":"alice","enabled":true,"ios_pairing_enabled":true,"line_ids":[],"revision":1}`, true},
		{"rename only", `{"username":"alice2","enabled":true,"ios_pairing_enabled":true,"line_ids":["line-1"],"revision":1}`, false},
		{"add line", `{"username":"alice","enabled":true,"ios_pairing_enabled":true,"line_ids":["line-1","line-2"],"revision":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &fakeUserRepository{fakeRepository: &fakeRepository{}, users: []store.User{{ID: "member-1", Username: "alice", Role: auth.RoleMember, Enabled: true, IOSPairingEnabled: true, LineIDs: []string{"line-1"}, Revision: 1}}, updateUser: store.User{ID: "member-1", Username: "alice", Role: auth.RoleMember, Enabled: true, Revision: 2}}
			leases := &fakeCallLeases{revokedCallIDs: []string{"call-member"}}
			media := &fakeCallMedia{}
			communications := &fakeCommunications{}
			api, err := New(repository, Options{disableAuthentication: true, CallLeases: leases, Communications: communications, CallMedia: media})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/users/member-1", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if (media.closed == "call-member") != tc.wantRevoked {
				t.Fatalf("access revoked but media was not closed; revokedSubject=%q revokedHolder=%q endedCall=%q", leases.revokedSubjectID, leases.revokedHolderID, communications.endCallID)
			}
		})
	}
}

func TestPairingRevocationInvalidatesLiveLease(t *testing.T) {
	ctx := context.Background()
	db, err := platformdb.Open(ctx, platformdb.Config{TargetPath: filepath.Join(t.TempDir(), "review.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := store.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO modemdeck_lines(line_id,phone_number) VALUES('line_review','+819011111111')`); err != nil {
		t.Fatal(err)
	}
	member, err := repository.CreateMember(ctx, store.CreateMemberInput{Username: "review", PasswordHash: "hash", IOSPairingEnabled: true, LineIDs: []string{"line_review"}})
	if err != nil {
		t.Fatal(err)
	}
	digest := mobilepairing.TokenDigest{1}
	if _, err = repository.CreateIOSPairingCredential(ctx, member.ID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ConfirmIOSPairingCredential(ctx, digest, mobilepairing.DeviceInfo{Name: "review"}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.IOSPairingPrincipalByTokenDigest(ctx, digest); err != nil || !found {
		t.Fatalf("setup credential found=%v error=%v", found, err)
	}
	now := time.Now().UTC()
	communications := &fakeCommunications{}
	leases, err := calllease.New(repository, communications, calllease.Options{Duration: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err = leases.ReconcileAuthoritativeCalls(ctx, nil); err != nil {
		t.Fatal(err)
	}
	_, err = repository.UpsertHardwareCall(ctx, store.HardwareCall{AppID: "call-review", LineID: "line_review", EndpointLineID: "endpoint-review", EndpointCallID: "endpoint-call-review", Number: "+819012345678", Direction: "incoming", Phase: "ringing", ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	holder := callLeaseHolderForMobileCredential(digest)
	if _, err = leases.ClaimFor(ctx, "call-review", calllease.Owner{HolderID: holder, SubjectID: member.ID}); err != nil {
		t.Fatal(err)
	}
	leases.MediaConnected("call-review")
	media := &fakeCallMedia{}
	api, err := New(repository, Options{disableAuthentication: true, CallLeases: leases, Communications: communications, CallMedia: media})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+member.ID, bytes.NewBufferString(`{"username":"review","enabled":true,"ios_pairing_enabled":false,"line_ids":["line_review"],"revision":1}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if _, found, err := repository.IOSPairingPrincipalByTokenDigest(ctx, digest); err != nil || found {
		t.Fatalf("credential should be revoked: found=%v error=%v", found, err)
	}
	now = now.Add(time.Hour)
	if err := leases.Require(ctx, "call-review", holder); err == nil {
		t.Fatalf("credential is revoked but real call lease still accepts the former owner; media.closed=%q endedCall=%q", media.closed, communications.endCallID)
	}
}
