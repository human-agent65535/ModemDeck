package httpapi

import "github.com/human-agent65535/modemdeck/internal/store"

type incomingMessageEventResponse struct {
	ID        uint64 `json:"id"`
	EventKey  string `json:"event_key"`
	MessageID string `json:"message_id"`
	ThreadKey string `json:"thread_key"`
	LineID    string `json:"line_id"`
	Peer      string `json:"peer"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type lineSummaryResponse struct {
	ID                       string                 `json:"id"`
	ICCID                    string                 `json:"iccid"`
	LineLabel                string                 `json:"line_label"`
	LineColor                store.LineColor        `json:"line_color"`
	IMSI                     string                 `json:"imsi"`
	PhoneNumber              string                 `json:"phone_number"`
	Operator                 string                 `json:"operator"`
	HomeOperatorCode         string                 `json:"home_operator_code"`
	HomeOperatorName         string                 `json:"home_operator_name"`
	ServingOperatorCode      string                 `json:"serving_operator_code"`
	ServingOperatorName      string                 `json:"serving_operator_name"`
	RegistrationStateKnown   bool                   `json:"registration_state_known"`
	RegistrationStateCode    uint32                 `json:"registration_state_code"`
	RegistrationState        string                 `json:"registration_state"`
	Roaming                  bool                   `json:"roaming"`
	EmergencyOnly            bool                   `json:"emergency_only"`
	DeviceIMEI               string                 `json:"device_imei"`
	DeviceName               string                 `json:"device_name"`
	Model                    string                 `json:"model"`
	Firmware                 string                 `json:"firmware"`
	HardwareRevision         string                 `json:"hardware_revision,omitempty"`
	PrimaryPort              string                 `json:"primary_port,omitempty"`
	Ports                    []store.HardwarePort   `json:"ports,omitempty"`
	AccessTechnologies       *uint32                `json:"access_technologies,omitempty"`
	State                    string                 `json:"state"`
	RadioDesiredEnabled      bool                   `json:"radio_desired_enabled"`
	RadioDesiredEnabledKnown bool                   `json:"radio_desired_enabled_known"`
	Signal                   *uint32                `json:"signal_quality,omitempty"`
	SignalSNR                *float64               `json:"signal_snr,omitempty"`
	Capabilities             store.LineCapabilities `json:"capabilities"`
}

func lineSummaryResponseFromStore(line store.LineSummary) lineSummaryResponse {
	return lineSummaryResponse{
		ID:                       line.ID,
		ICCID:                    line.ICCID,
		LineLabel:                line.LineLabel,
		LineColor:                line.LineColor,
		IMSI:                     line.IMSI,
		PhoneNumber:              line.PhoneNumber,
		Operator:                 line.Operator,
		HomeOperatorCode:         line.HomeOperatorCode,
		HomeOperatorName:         line.HomeOperatorName,
		ServingOperatorCode:      line.ServingOperatorCode,
		ServingOperatorName:      line.ServingOperatorName,
		RegistrationStateKnown:   line.RegistrationStateKnown,
		RegistrationStateCode:    line.RegistrationStateCode,
		RegistrationState:        line.RegistrationState,
		Roaming:                  line.Roaming,
		EmergencyOnly:            line.EmergencyOnly,
		DeviceIMEI:               line.DeviceIMEI,
		DeviceName:               line.DeviceName,
		Model:                    line.Model,
		Firmware:                 line.Firmware,
		HardwareRevision:         line.HardwareRevision,
		PrimaryPort:              line.PrimaryPort,
		Ports:                    line.Ports,
		AccessTechnologies:       line.AccessTechnologies,
		State:                    line.State,
		RadioDesiredEnabled:      line.RadioDesiredEnabled,
		RadioDesiredEnabledKnown: line.RadioDesiredEnabledKnown,
		Signal:                   line.Signal,
		SignalSNR:                line.SignalSNR,
		Capabilities:             line.Capabilities,
	}
}

func lineSummaryResponses(lines []store.LineSummary) []lineSummaryResponse {
	result := make([]lineSummaryResponse, 0, len(lines))
	for _, line := range lines {
		result = append(result, lineSummaryResponseFromStore(line))
	}
	return result
}

type messageThreadResponse struct {
	Key           string `json:"key"`
	LineID        string `json:"line_id"`
	Peer          string `json:"peer"`
	ContactID     string `json:"contact_id,omitempty"`
	ContactName   string `json:"contact_name,omitempty"`
	LastMessageID int64  `json:"last_message_id"`
	LastTimestamp string `json:"last_timestamp"`
	LastContent   string `json:"last_content,omitempty"`
	LastType      int64  `json:"last_type"`
	UnreadCount   int64  `json:"unread_count"`
}

func messageThreadResponses(threads []store.MessageThread) []messageThreadResponse {
	result := make([]messageThreadResponse, 0, len(threads))
	for _, thread := range threads {
		result = append(result, messageThreadResponse{
			Key:           thread.Key,
			LineID:        thread.LineID,
			Peer:          thread.Peer,
			ContactID:     thread.ContactID,
			ContactName:   thread.ContactName,
			LastMessageID: thread.LastMessageID,
			LastTimestamp: thread.LastTimestamp,
			LastContent:   thread.LastContent,
			LastType:      thread.LastType,
			UnreadCount:   thread.UnreadCount,
		})
	}
	return result
}

type messageResponseItem struct {
	ID          int64  `json:"id"`
	RequestID   string `json:"request_id,omitempty"`
	LineID      string `json:"line_id"`
	Peer        string `json:"peer"`
	Direction   string `json:"direction"`
	Sender      string `json:"sender,omitempty"`
	Recipient   string `json:"recipient,omitempty"`
	Content     string `json:"content"`
	Type        int64  `json:"type"`
	Status      int64  `json:"status"`
	State       string `json:"state,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
	Revision    int64  `json:"revision"`
	Timestamp   string `json:"timestamp"`
	CreatedAt   string `json:"created_at,omitempty"`
}

func messageResponseItemFromStore(message store.Message) messageResponseItem {
	return messageResponseItem{
		ID:          message.ID,
		RequestID:   message.RequestID,
		LineID:      message.LineID,
		Peer:        message.Peer,
		Direction:   message.Direction,
		Sender:      message.Sender,
		Recipient:   message.Recipient,
		Content:     message.Content,
		Type:        message.Type,
		Status:      message.Status,
		State:       message.State,
		FailureCode: message.FailureCode,
		Revision:    message.Revision,
		Timestamp:   message.Timestamp,
		CreatedAt:   message.CreatedAt,
	}
}

func messageResponseItems(messages []store.Message) []messageResponseItem {
	result := make([]messageResponseItem, 0, len(messages))
	for _, message := range messages {
		result = append(result, messageResponseItemFromStore(message))
	}
	return result
}

type callRecordResponse struct {
	ID              string  `json:"id"`
	RequestID       string  `json:"request_id,omitempty"`
	LineID          string  `json:"line_id"`
	Direction       string  `json:"direction"`
	RemoteNumber    string  `json:"remote_number"`
	ContactID       string  `json:"contact_id,omitempty"`
	ContactName     string  `json:"contact_name,omitempty"`
	Phase           string  `json:"phase,omitempty"`
	Revision        int64   `json:"revision"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       string  `json:"started_at"`
	UpdatedAt       string  `json:"updated_at"`
	ActiveAt        *string `json:"active_at,omitempty"`
	EndedAt         string  `json:"ended_at,omitempty"`
	EndReason       string  `json:"end_reason,omitempty"`
	FailureCode     string  `json:"failure_code,omitempty"`
	Bearer          string  `json:"bearer,omitempty"`
	StateReason     string  `json:"state_reason,omitempty"`
	StateReasonCode int64   `json:"state_reason_code,omitempty"`
	Multiparty      bool    `json:"multiparty"`
	MediaAvailable  bool    `json:"media_available"`
	DurationSeconds int64   `json:"duration_seconds"`
	Missed          bool    `json:"missed"`
	Read            bool    `json:"read"`
}

func callRecordResponses(calls []store.Call) []callRecordResponse {
	result := make([]callRecordResponse, 0, len(calls))
	for _, call := range calls {
		result = append(result, callRecordResponse{
			ID:              call.ID,
			RequestID:       call.RequestID,
			LineID:          call.LineID,
			Direction:       call.Direction,
			RemoteNumber:    call.RemoteNumber,
			ContactID:       call.ContactID,
			ContactName:     call.ContactName,
			Phase:           call.Phase,
			Revision:        call.Revision,
			CreatedAt:       call.CreatedAt,
			StartedAt:       call.StartedAt,
			UpdatedAt:       call.UpdatedAt,
			ActiveAt:        call.ActiveAt,
			EndedAt:         call.EndedAt,
			EndReason:       call.EndReason,
			FailureCode:     call.FailureCode,
			Bearer:          call.Bearer,
			StateReason:     call.StateReason,
			StateReasonCode: call.StateReasonCode,
			Multiparty:      call.Multiparty,
			MediaAvailable:  call.MediaAvailable,
			DurationSeconds: call.DurationSeconds,
			Missed:          call.Missed,
			Read:            call.Read,
		})
	}
	return result
}

type recordingCallResponse struct {
	ID              string `json:"id"`
	LineID          string `json:"line_id"`
	Direction       string `json:"direction"`
	RemoteNumber    string `json:"remote_number"`
	ContactID       string `json:"contact_id,omitempty"`
	ContactName     string `json:"contact_name,omitempty"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	DurationSeconds int64  `json:"duration_seconds"`
	Missed          bool   `json:"missed"`
	EndReason       string `json:"end_reason,omitempty"`
	FailureCode     string `json:"failure_code,omitempty"`
}

type recordingEntryResponse struct {
	Segment  store.RecordingSegment `json:"segment"`
	Call     recordingCallResponse  `json:"call"`
	Playable bool                   `json:"playable"`
}

func recordingEntryResponses(entries []store.RecordingEntry) []recordingEntryResponse {
	result := make([]recordingEntryResponse, 0, len(entries))
	for _, entry := range entries {
		result = append(result, recordingEntryResponse{
			Segment: entry.Segment,
			Call: recordingCallResponse{
				ID:              entry.Call.ID,
				LineID:          entry.Call.LineID,
				Direction:       entry.Call.Direction,
				RemoteNumber:    entry.Call.RemoteNumber,
				ContactID:       entry.Call.ContactID,
				ContactName:     entry.Call.ContactName,
				StartedAt:       entry.Call.StartedAt,
				EndedAt:         entry.Call.EndedAt,
				DurationSeconds: entry.Call.DurationSeconds,
				Missed:          entry.Call.Missed,
				EndReason:       entry.Call.EndReason,
				FailureCode:     entry.Call.FailureCode,
			},
			Playable: entry.Playable,
		})
	}
	return result
}
