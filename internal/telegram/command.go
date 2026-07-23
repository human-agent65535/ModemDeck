package telegram

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type CommandKind string

const (
	CommandHelp  CommandKind = "help"
	CommandLines CommandKind = "lines"
	CommandSMS   CommandKind = "sms"
	CommandCall  CommandKind = "call"
	CommandReply CommandKind = "reply"
)

type Command struct {
	Kind      CommandKind
	TargetBot string
	LineID    string
	Number    string
	Body      string
	Limit     int
}

type CommandError struct {
	Code string
}

func (e *CommandError) Error() string {
	return "invalid Telegram command: " + e.Code
}

func ParseCommand(text string) (Command, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return Command{}, &CommandError{Code: "not_a_command"}
	}

	token, rest := cutField(text)
	nameWithTarget := strings.TrimPrefix(token, "/")
	name, target, hasTarget := strings.Cut(nameWithTarget, "@")
	if name == "" || (hasTarget && target == "") {
		return Command{}, &CommandError{Code: "invalid_name"}
	}
	command := Command{TargetBot: target}

	switch strings.ToLower(name) {
	case "help", "start":
		if rest != "" {
			return Command{}, &CommandError{Code: "help_has_arguments"}
		}
		command.Kind = CommandHelp
	case "lines":
		if rest != "" {
			return Command{}, &CommandError{Code: "lines_has_arguments"}
		}
		command.Kind = CommandLines
	case "sms":
		return parseSMSCommand(command, rest)
	case "call":
		return parseCallCommand(command, rest)
	case "reply":
		return parseReplyCommand(command, rest)
	default:
		return Command{}, &CommandError{Code: "unknown_command"}
	}
	return command, nil
}

func parseSMSCommand(command Command, rest string) (Command, error) {
	command.Kind = CommandSMS
	command.Limit = DefaultSMSQueryLimit
	if rest == "" {
		return command, nil
	}
	fields := strings.Fields(rest)
	if len(fields) > 2 {
		return Command{}, &CommandError{Code: "sms_too_many_arguments"}
	}
	if err := validateLineID(fields[0]); err != nil {
		return Command{}, &CommandError{Code: "invalid_line"}
	}
	command.LineID = fields[0]
	if len(fields) == 2 {
		limit, err := parseSMSLimit(fields[1])
		if err != nil {
			return Command{}, err
		}
		command.Limit = limit
	}
	return command, nil
}

func parseCallCommand(command Command, rest string) (Command, error) {
	lineID, numberValue := cutField(rest)
	if lineID == "" || numberValue == "" {
		return Command{}, &CommandError{Code: "call_requires_line_and_number"}
	}
	if err := validateLineID(lineID); err != nil {
		return Command{}, &CommandError{Code: "invalid_line"}
	}
	number, err := NormalizePhone(numberValue)
	if err != nil {
		return Command{}, &CommandError{Code: "invalid_phone"}
	}
	command.Kind = CommandCall
	command.LineID = lineID
	command.Number = number
	return command, nil
}

func parseReplyCommand(command Command, rest string) (Command, error) {
	lineID, afterLine := cutField(rest)
	numberValue, body := cutField(afterLine)
	if lineID == "" || numberValue == "" || body == "" {
		return Command{}, &CommandError{Code: "reply_requires_line_number_and_body"}
	}
	if err := validateLineID(lineID); err != nil {
		return Command{}, &CommandError{Code: "invalid_line"}
	}
	number, err := NormalizePhone(numberValue)
	if err != nil {
		return Command{}, &CommandError{Code: "invalid_phone"}
	}
	if utf8.RuneCountInString(body) > MaxSMSBodyRunes {
		return Command{}, &CommandError{Code: "sms_body_too_long"}
	}
	command.Kind = CommandReply
	command.LineID = lineID
	command.Number = number
	command.Body = body
	return command, nil
}

func parseSMSLimit(value string) (int, error) {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > MaxSMSQueryLimit {
		return 0, &CommandError{Code: "invalid_sms_limit"}
	}
	return limit, nil
}

func cutField(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	for index, ch := range value {
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			return value[:index], strings.TrimSpace(value[index:])
		}
	}
	return value, ""
}
