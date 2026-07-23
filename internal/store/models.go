package store

import "time"

const (
	DefaultQueryLimit           = 50
	MaxQueryLimit               = 200
	MaxContactDisplayNameLength = 200
	MaxContactNotesLength       = 4000
	MaxContactPhoneLabelLength  = 50
	MaxContactPhoneNumberLength = 64
	MaxContactPhones            = 50
)

type ContactQuery struct {
	Search string
	Limit  int
}

type Contact struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"display_name"`
	Notes       string         `json:"notes"`
	Revision    int64          `json:"revision"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Phones      []ContactPhone `json:"phones"`
}

type ContactPhone struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	OriginalNumber string `json:"original_number"`
	CanonicalE164  string `json:"canonical_e164"`
	Primary        bool   `json:"primary"`
}

type ContactInput struct {
	DisplayName string              `json:"display_name"`
	Notes       string              `json:"notes"`
	Revision    int64               `json:"revision"`
	Phones      []ContactPhoneInput `json:"phones"`
}

type ContactPhoneInput struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Number  string `json:"number"`
	Primary bool   `json:"primary"`
}

type ThreadQuery struct {
	Search string
	Limit  int
}

type MessageThread struct {
	IMSI          string `json:"imsi"`
	ICCID         string `json:"iccid"`
	Peer          string `json:"peer"`
	ContactID     string `json:"contact_id"`
	ContactName   string `json:"contact_name"`
	LastMessageID int64  `json:"last_message_id"`
	LastTimestamp string `json:"last_timestamp"`
	LastContent   string `json:"last_content"`
	LastType      int64  `json:"last_type"`
	UnreadCount   int64  `json:"unread_count"`
}

type MessageQuery struct {
	ICCID string
	Peer  string
	Limit int
}

type Message struct {
	ID                int64  `json:"id"`
	RequestID         string `json:"request_id"`
	LineID            string `json:"line_id"`
	EndpointMessageID string `json:"endpoint_message_id"`
	IMSI              string `json:"imsi"`
	ICCID             string `json:"iccid"`
	Peer              string `json:"peer"`
	Direction         string `json:"direction"`
	LocalPhone        string `json:"local_phone"`
	Sender            string `json:"sender"`
	Recipient         string `json:"recipient"`
	Content           string `json:"content"`
	Type              int64  `json:"type"`
	Status            int64  `json:"status"`
	State             string `json:"state"`
	FailureCode       string `json:"failure_code"`
	Revision          int64  `json:"revision"`
	Timestamp         string `json:"timestamp"`
	CreatedAt         string `json:"created_at"`
}

type CallKind string

const (
	CallKindAll      CallKind = "all"
	CallKindIncoming CallKind = "incoming"
	CallKindOutgoing CallKind = "outgoing"
	CallKindMissed   CallKind = "missed"
)

type CallQuery struct {
	Kind   CallKind
	Search string
	Limit  int
}

type Call struct {
	ID              string  `json:"id"`
	RequestID       string  `json:"request_id"`
	DeviceID        string  `json:"device_id"`
	Direction       string  `json:"direction"`
	RemoteNumber    string  `json:"remote_number"`
	ContactID       string  `json:"contact_id"`
	ContactName     string  `json:"contact_name"`
	EndpointID      string  `json:"endpoint_id"`
	EndpointCallID  string  `json:"endpoint_call_id"`
	Phase           string  `json:"phase"`
	Revision        int64   `json:"revision"`
	CreatedAt       string  `json:"created_at"`
	StartedAt       string  `json:"started_at"`
	UpdatedAt       string  `json:"updated_at"`
	ActiveAt        *string `json:"active_at"`
	EndedAt         string  `json:"ended_at"`
	EndReason       string  `json:"end_reason"`
	FailureCode     string  `json:"failure_code"`
	Bearer          string  `json:"bearer"`
	DurationSeconds int64   `json:"duration_seconds"`
	Missed          bool    `json:"missed"`
}

type Device struct {
	IMEI          string   `json:"imei"`
	Alias         string   `json:"alias"`
	Model         string   `json:"model"`
	Firmware      string   `json:"firmware"`
	Port          string   `json:"port"`
	PublicIP      string   `json:"public_ip"`
	PrivateIP     string   `json:"private_ip"`
	PublicIPv6    string   `json:"public_ipv6"`
	PrivateIPv6   string   `json:"private_ipv6"`
	CurrentICCID  string   `json:"current_iccid"`
	SIMInserted   bool     `json:"sim_inserted"`
	SignalQuality *uint32  `json:"signal_quality,omitempty"`
	SignalDBM     int64    `json:"signal_dbm"`
	SignalRSRQ    int64    `json:"signal_rsrq"`
	SignalRSRP    int64    `json:"signal_rsrp"`
	LastSeen      string   `json:"last_seen"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	SIM           *SIMCard `json:"sim,omitempty"`
}

type SIMCard struct {
	ICCID         string `json:"iccid"`
	IMSI          string `json:"imsi"`
	PhoneNumber   string `json:"phone_number"`
	Operator      string `json:"operator"`
	CurrentIMEI   string `json:"current_imei"`
	RegStatus     int64  `json:"reg_status"`
	RegStatusText string `json:"reg_status_text"`
	LAC           string `json:"lac"`
	CellID        string `json:"cell_id"`
	APN           string `json:"apn"`
	IMSStatus     int64  `json:"ims_status"`
	LastSeen      string `json:"last_seen"`
}

type LineSummary struct {
	ID           string           `json:"id"`
	ICCID        string           `json:"iccid"`
	IMSI         string           `json:"imsi"`
	PhoneNumber  string           `json:"phone_number"`
	Operator     string           `json:"operator"`
	DeviceIMEI   string           `json:"device_imei"`
	DeviceAlias  string           `json:"device_alias"`
	Model        string           `json:"model"`
	Firmware     string           `json:"firmware"`
	State        string           `json:"state"`
	Signal       *uint32          `json:"signal_quality,omitempty"`
	Capabilities LineCapabilities `json:"capabilities"`
}

type LineCapabilities struct {
	Dial        bool `json:"dial"`
	AnswerCall  bool `json:"answer_call"`
	HangupCall  bool `json:"hangup_call"`
	RejectCall  bool `json:"reject_call"`
	SendDTMF    bool `json:"send_dtmf"`
	SendMessage bool `json:"send_message"`
	Media       bool `json:"media"`
}

type HardwareSnapshot struct {
	Revision   uint64
	ObservedAt time.Time
	Lines      []HardwareLine
	Calls      []HardwareCall
	Messages   []HardwareMessage
}

type HardwareLine struct {
	ID                  string
	Manufacturer        string
	Model               string
	Firmware            string
	DeviceIdentifier    string
	EquipmentIdentifier string
	PhysicalDevice      string
	PrimaryPort         string
	State               string
	SignalKnown         bool
	SignalQuality       uint32
	PhoneNumber         string
	ICCID               string
	IMSI                string
	Operator            string
	Capabilities        LineCapabilities
}

type HardwareCall struct {
	AppID          string
	RequestID      string
	LineID         string
	EndpointCallID string
	Number         string
	Direction      string
	Phase          string
	Bearer         string
	Revision       int64
	ObservedAt     time.Time
}

type HardwareMessage struct {
	RequestID         string
	LineID            string
	EndpointMessageID string
	IMSI              string
	ICCID             string
	LocalPhone        string
	Number            string
	Text              string
	Direction         string
	State             string
	StateCode         int64
	Revision          int64
	Timestamp         time.Time
	ObservedAt        time.Time
}

type CallControlTarget struct {
	AppID          string
	LineID         string
	EndpointCallID string
	Number         string
	Direction      string
	Phase          string
	Bearer         string
	Revision       int64
}
