package store

const (
	DefaultQueryLimit = 50
	MaxQueryLimit     = 200
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
	ID         int64  `json:"id"`
	IMSI       string `json:"imsi"`
	ICCID      string `json:"iccid"`
	Peer       string `json:"peer"`
	Direction  string `json:"direction"`
	LocalPhone string `json:"local_phone"`
	Sender     string `json:"sender"`
	Recipient  string `json:"recipient"`
	Content    string `json:"content"`
	Type       int64  `json:"type"`
	Status     int64  `json:"status"`
	Timestamp  string `json:"timestamp"`
	CreatedAt  string `json:"created_at"`
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
	DurationSeconds int64   `json:"duration_seconds"`
	Missed          bool    `json:"missed"`
}

type Device struct {
	IMEI         string   `json:"imei"`
	Alias        string   `json:"alias"`
	Model        string   `json:"model"`
	Firmware     string   `json:"firmware"`
	Port         string   `json:"port"`
	PublicIP     string   `json:"public_ip"`
	PrivateIP    string   `json:"private_ip"`
	PublicIPv6   string   `json:"public_ipv6"`
	PrivateIPv6  string   `json:"private_ipv6"`
	CurrentICCID string   `json:"current_iccid"`
	SIMInserted  bool     `json:"sim_inserted"`
	SignalDBM    int64    `json:"signal_dbm"`
	SignalRSRQ   int64    `json:"signal_rsrq"`
	SignalRSRP   int64    `json:"signal_rsrp"`
	LastSeen     string   `json:"last_seen"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	SIM          *SIMCard `json:"sim,omitempty"`
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
	ICCID       string `json:"iccid"`
	IMSI        string `json:"imsi"`
	PhoneNumber string `json:"phone_number"`
	Operator    string `json:"operator"`
	DeviceIMEI  string `json:"device_imei"`
	DeviceAlias string `json:"device_alias"`
}
