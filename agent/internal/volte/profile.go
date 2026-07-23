package volte

import (
	"fmt"
	"strings"
	"time"
)

type Profile struct {
	ID               string
	Identity         Identity
	OperationTimeout time.Duration
	Read             ReadMethod
	Write            WriteMethod
}

func (p Profile) validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile id is required")
	}
	if p.ID != strings.TrimSpace(p.ID) {
		return fmt.Errorf("profile id must not contain surrounding whitespace")
	}
	if err := validateIdentity(p.Identity); err != nil {
		return err
	}
	if p.OperationTimeout <= 0 {
		return fmt.Errorf("operation timeout must be positive")
	}
	if p.Read == nil && p.Write == nil {
		return fmt.Errorf("at least one read or write method is required")
	}
	if p.Read == nil && p.Write != nil {
		return fmt.Errorf("a writable profile requires a read method for verification")
	}
	if p.Read != nil {
		if err := p.Read.validate(); err != nil {
			return err
		}
	}
	if p.Write != nil {
		if err := p.Write.validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateIdentity(identity Identity) error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "manufacturer", value: identity.Manufacturer},
		{name: "model", value: identity.Model},
		{name: "firmware", value: identity.Firmware},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
		if field.value != strings.TrimSpace(field.value) {
			return fmt.Errorf("%s must not contain surrounding whitespace", field.name)
		}
		if strings.ContainsAny(field.value, "*?") {
			return fmt.Errorf("%s must be exact and cannot contain wildcard characters", field.name)
		}
	}
	return nil
}

func (p Profile) capability() Capability {
	capability := Capability{
		Supported: true,
		ProfileID: p.ID,
		Identity:  p.Identity,
		Readable:  p.Read != nil,
		Writable:  p.Write != nil,
	}
	if p.Read != nil {
		capability.ReadProtocol = p.Read.Protocol()
	}
	if p.Write != nil {
		capability.WriteProtocol = p.Write.Protocol()
	}
	return capability
}
