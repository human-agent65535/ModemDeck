package modemmanager

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type instanceIDs struct {
	bootEpoch string
	secret    []byte
}

func newInstanceIDs() (*instanceIDs, error) {
	epoch := make([]byte, 12)
	if _, err := rand.Read(epoch); err != nil {
		return nil, fmt.Errorf("generate agent boot epoch: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate agent object ID secret: %w", err)
	}
	return &instanceIDs{
		bootEpoch: hex.EncodeToString(epoch),
		secret:    secret,
	}, nil
}

func newInstanceIDsForTest(bootEpoch string, secret []byte) *instanceIDs {
	return &instanceIDs{
		bootEpoch: bootEpoch,
		secret:    append([]byte(nil), secret...),
	}
}

func (ids *instanceIDs) lineID(path dbus.ObjectPath, line domain.Line) string {
	identity := strings.Join([]string{
		line.EquipmentIdentifier,
		line.DeviceIdentifier,
		line.PhysicalDevice,
		line.Device,
		string(path),
	}, "\x00")
	sum := sha256.Sum256([]byte("line\x00" + identity))
	return "line_" + base64.RawURLEncoding.EncodeToString(sum[:18])
}

func (ids *instanceIDs) callID(path dbus.ObjectPath) string {
	return ids.objectID("call", path)
}

func (ids *instanceIDs) messageID(path dbus.ObjectPath) string {
	return ids.objectID("message", path)
}

func (ids *instanceIDs) objectID(kind string, path dbus.ObjectPath) string {
	mac := hmac.New(sha256.New, ids.secret)
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(path))
	digest := mac.Sum(nil)
	return kind + "_" + ids.bootEpoch + "_" + base64.RawURLEncoding.EncodeToString(digest[:18])
}
