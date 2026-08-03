package mobilepairing

type DeviceInfo struct {
	Name            string `json:"device_name,omitempty"`
	Model           string `json:"device_model,omitempty"`
	ModelIdentifier string `json:"device_model_identifier,omitempty"`
	OSName          string `json:"os_name,omitempty"`
	OSVersion       string `json:"os_version,omitempty"`
	AppVersion      string `json:"app_version,omitempty"`
	AppBuild        string `json:"app_build,omitempty"`
}
