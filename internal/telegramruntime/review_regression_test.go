package telegramruntime

import (
	"context"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/telegram"
	"testing"
)

func TestSuccessfulTelegramSendPublishesDurableRevision(t *testing.T) {
	events := runtimeevents.NewHub()
	err := (adapters{communications: fakeCommunications{}, runtimeEvents: events}).SendSMS(context.Background(), telegram.SMSRequest{RequestID: "review-request", LineID: "line-1", To: "+819012345678", Body: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if events.Current().DataRevision == 0 {
		t.Fatal("successful Telegram SendSMS did not invalidate message collection revision")
	}
}
