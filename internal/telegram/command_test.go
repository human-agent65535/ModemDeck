package telegram

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Command
		wantErr string
	}{
		{
			name:  "help",
			input: "/help",
			want:  Command{Kind: CommandHelp},
		},
		{
			name:  "start aliases help",
			input: "/start@MyBot",
			want:  Command{Kind: CommandHelp, TargetBot: "MyBot"},
		},
		{
			name:  "list",
			input: "  /list  ",
			want:  Command{Kind: CommandList},
		},
		{
			name:  "legacy lines alias",
			input: "  /lines  ",
			want:  Command{Kind: CommandList},
		},
		{
			name:  "sms defaults",
			input: "/sms",
			want:  Command{Kind: CommandSMS, Limit: DefaultSMSQueryLimit},
		},
		{
			name:  "sms line",
			input: "/sms 10",
			want:  Command{Kind: CommandSMS, LineID: "10", Limit: DefaultSMSQueryLimit},
		},
		{
			name:  "sms line and limit",
			input: "/sms line-a 20",
			want:  Command{Kind: CommandSMS, LineID: "line-a", Limit: 20},
		},
		{
			name:  "call defaults",
			input: "/call",
			want: Command{
				Kind:  CommandCall,
				Limit: DefaultCallQueryLimit,
			},
		},
		{
			name:  "call limit",
			input: "/call 20",
			want: Command{
				Kind:  CommandCall,
				Limit: 20,
			},
		},
		{
			name:  "reply preserves body",
			input: "/reply line-a +81-80-1234-5678  Hello from Telegram  ",
			want: Command{
				Kind:   CommandReply,
				LineID: "line-a",
				Number: "+81-80-1234-5678",
				Body:   "Hello from Telegram",
			},
		},
		{name: "not command", input: "hello", wantErr: "not_a_command"},
		{name: "unknown", input: "/proxy", wantErr: "unknown_command"},
		{name: "help args", input: "/help now", wantErr: "help_has_arguments"},
		{name: "sms limit low", input: "/sms line-a 0", wantErr: "invalid_sms_limit"},
		{name: "sms limit high", input: "/sms line-a 21", wantErr: "invalid_sms_limit"},
		{name: "sms extra", input: "/sms line-a 10 extra", wantErr: "sms_too_many_arguments"},
		{name: "call limit low", input: "/call 0", wantErr: "invalid_call_limit"},
		{name: "call limit high", input: "/call 21", wantErr: "invalid_call_limit"},
		{name: "call extra", input: "/call 10 extra", wantErr: "call_too_many_arguments"},
		{name: "reply missing body", input: "/reply line-a +818012345678", wantErr: "reply_requires_line_number_and_body"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCommand(test.input)
			if test.wantErr != "" {
				if err == nil {
					t.Fatal("ParseCommand() returned nil error")
				}
				var commandErr *CommandError
				if !errors.As(err, &commandErr) {
					t.Fatalf("error type = %T, want *CommandError", err)
				}
				if commandErr.Code != test.wantErr {
					t.Fatalf("error code = %q, want %q", commandErr.Code, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCommand() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ParseCommand() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseReplyBodyLimit(t *testing.T) {
	t.Parallel()

	_, err := ParseCommand("/reply line-a +818012345678 " + strings.Repeat("x", MaxSMSBodyRunes+1))
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.Code != "sms_body_too_long" {
		t.Fatalf("error = %v, want sms_body_too_long", err)
	}
}
