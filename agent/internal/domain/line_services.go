package domain

import (
	"context"
	"time"
)

type SIMType string

const (
	SIMTypeUnknown  SIMType = "unknown"
	SIMTypePhysical SIMType = "physical"
	SIMTypeESIM     SIMType = "esim"
)

type ESIMStatus string

const (
	ESIMStatusUnknown      ESIMStatus = "unknown"
	ESIMStatusNoProfiles   ESIMStatus = "no_profiles"
	ESIMStatusWithProfiles ESIMStatus = "with_profiles"
)

type SIMSlot struct {
	Index      uint32     `json:"index"`
	Present    bool       `json:"present"`
	Current    bool       `json:"current"`
	SIMType    SIMType    `json:"sim_type"`
	ESIMStatus ESIMStatus `json:"esim_status"`
	EIDMasked  string     `json:"eid,omitempty"`
}

type SIMProfileManagementCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

// SIMStatus contains a complete slot inventory only when SIMSlotsKnown is
// true. Otherwise SIMSlots may contain the current SIM as a synthetic entry
// with an ordinal Index.
type SIMStatus struct {
	LineID                 string                         `json:"line_id"`
	Present                bool                           `json:"present"`
	Active                 bool                           `json:"active"`
	Identifier             string                         `json:"identifier"`
	IMSI                   string                         `json:"imsi"`
	SIMType                SIMType                        `json:"sim_type"`
	ESIMStatus             ESIMStatus                     `json:"esim_status"`
	EIDMasked              string                         `json:"eid,omitempty"`
	SIMSlots               []SIMSlot                      `json:"sim_slots"`
	SIMSlotsKnown          bool                           `json:"sim_slots_known"`
	PrimarySIMSlot         uint32                         `json:"primary_sim_slot"`
	PrimarySIMSlotKnown    bool                           `json:"primary_sim_slot_known"`
	CurrentSIMSlot         uint32                         `json:"current_sim_slot"`
	CurrentSIMSlotKnown    bool                           `json:"current_sim_slot_known"`
	ProfileManagement      SIMProfileManagementCapability `json:"profile_management"`
	HomeOperatorCode       string                         `json:"home_operator_code"`
	HomeOperatorName       string                         `json:"home_operator_name"`
	HomeCountryISO         string                         `json:"home_country_iso"`
	ServingOperatorCode    string                         `json:"serving_operator_code"`
	ServingOperatorName    string                         `json:"serving_operator_name"`
	ServingCountryISO      string                         `json:"serving_country_iso"`
	RegistrationStateKnown bool                           `json:"registration_state_known"`
	RegistrationStateCode  uint32                         `json:"registration_state_code"`
	RegistrationState      string                         `json:"registration_state"`
	Roaming                bool                           `json:"roaming"`
	OperatorIdentifier     string                         `json:"operator_identifier"`
	OperatorName           string                         `json:"operator_name"`
	UnlockRequired         string                         `json:"unlock_required"`
	UnlockRequiredCode     uint32                         `json:"unlock_required_code"`
	UnlockRetries          map[string]uint32              `json:"unlock_retries"`
	ObservedAt             time.Time                      `json:"observed_at"`
}

type SIMOperation string

const (
	SIMSendPIN   SIMOperation = "send_pin"
	SIMSendPUK   SIMOperation = "send_puk"
	SIMEnablePIN SIMOperation = "enable_pin"
	SIMChangePIN SIMOperation = "change_pin"
)

type SIMCommandRequest struct {
	RequestID string       `json:"request_id"`
	LineID    string       `json:"-"`
	Operation SIMOperation `json:"operation"`
	PIN       string       `json:"pin,omitempty"`
	PUK       string       `json:"puk,omitempty"`
	NewPIN    string       `json:"new_pin,omitempty"`
	Enabled   *bool        `json:"enabled,omitempty"`
}

type ConnectionProfile struct {
	ProfileID            int32  `json:"profile_id"`
	ProfileName          string `json:"profile_name"`
	APN                  string `json:"apn"`
	IPFamily             string `json:"ip_family"`
	IPType               uint32 `json:"ip_type"`
	APNType              uint32 `json:"apn_type"`
	AllowedAuth          uint32 `json:"allowed_auth"`
	User                 string `json:"user,omitempty"`
	AccessTypePreference uint32 `json:"access_type_preference"`
	RoamingAllowance     uint32 `json:"roaming_allowance"`
	ProfileSource        uint32 `json:"profile_source"`
}

type SaveConnectionProfileRequest struct {
	RequestID            string `json:"request_id"`
	LineID               string `json:"-"`
	ProfileID            *int32 `json:"profile_id,omitempty"`
	ProfileName          string `json:"profile_name,omitempty"`
	APN                  string `json:"apn,omitempty"`
	IPFamily             string `json:"ip_family,omitempty"`
	APNType              uint32 `json:"apn_type,omitempty"`
	AllowedAuth          uint32 `json:"allowed_auth,omitempty"`
	User                 string `json:"user,omitempty"`
	Password             string `json:"password,omitempty"`
	AccessTypePreference uint32 `json:"access_type_preference,omitempty"`
	RoamingAllowance     uint32 `json:"roaming_allowance,omitempty"`
}

type DeleteConnectionProfileRequest struct {
	RequestID   string `json:"request_id"`
	LineID      string `json:"-"`
	ProfileID   *int32 `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
}

type USSDStatus struct {
	LineID              string    `json:"line_id"`
	State               string    `json:"state"`
	StateCode           uint32    `json:"state_code"`
	NetworkNotification string    `json:"network_notification,omitempty"`
	NetworkRequest      string    `json:"network_request,omitempty"`
	ObservedAt          time.Time `json:"observed_at"`
}

type USSDAction string

const (
	USSDInitiate USSDAction = "initiate"
	USSDRespond  USSDAction = "respond"
	USSDCancel   USSDAction = "cancel"
)

type USSDRequest struct {
	RequestID string     `json:"request_id"`
	LineID    string     `json:"-"`
	Action    USSDAction `json:"action"`
	Command   string     `json:"command,omitempty"`
}

type USSDResponse struct {
	Response string `json:"response,omitempty"`
}

type LineServiceProvider interface {
	SIMStatus(context.Context, string) (SIMStatus, error)
	SIMCommand(context.Context, SIMCommandRequest) (CommandReceipt, error)
	ConnectionProfiles(context.Context, string) ([]ConnectionProfile, error)
	SaveConnectionProfile(context.Context, SaveConnectionProfileRequest) (ConnectionProfile, error)
	DeleteConnectionProfile(context.Context, DeleteConnectionProfileRequest) (CommandReceipt, error)
	USSDStatus(context.Context, string) (USSDStatus, error)
	USSDCommand(context.Context, USSDRequest) (USSDResponse, error)
}
