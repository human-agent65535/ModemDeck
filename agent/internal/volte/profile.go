package volte

import (
	"fmt"
	"strings"
	"time"
)

type Profile struct {
	ID                   string
	Matches              IdentityMatcher
	OperationTimeout     time.Duration
	Read                 ReadMethod
	Write                WriteMethod
	ApplyRequiresRestart bool
}

func (p Profile) validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("profile id is required")
	}
	if p.ID != strings.TrimSpace(p.ID) {
		return fmt.Errorf("profile id must not contain surrounding whitespace")
	}
	if p.Matches == nil {
		return fmt.Errorf("identity matcher is required")
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

func (p Profile) capability(identity Identity) Capability {
	capability := Capability{
		Supported:            true,
		ProfileID:            p.ID,
		Identity:             identity,
		Readable:             p.Read != nil,
		Writable:             p.Write != nil,
		ApplyRequiresRestart: p.ApplyRequiresRestart,
	}
	if p.Read != nil {
		capability.ReadProtocol = p.Read.Protocol()
	}
	if p.Write != nil {
		capability.WriteProtocol = p.Write.Protocol()
	}
	return capability
}
