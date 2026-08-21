package qdc507usb

import (
	"context"
	"crypto/md5" // #nosec G501 -- QDC507's legacy QADBKEY protocol requires MD5-crypt.
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	legacyQADBKeySecret = "SH_adb_quectel"
	legacyQADBKeyLength = 15
	md5CryptMagic       = "$1$"
	md5CryptAlphabet    = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var (
	legacyQADBKeyChallengePattern = regexp.MustCompile(`(?im)^\s*\+QADBKEY:\s*([0-9]{8})\s*$`)
	legacyQADBKeyValuePattern     = regexp.MustCompile(`^[0-9]{8}$`)
)

// Inhibitor gives the USB provisioner exclusive ownership of one physical
// modem while it uses the serial AT port directly.
type Inhibitor interface {
	Inhibit(context.Context, string) (func(context.Context) error, error)
}

// ATClient is the direct serial boundary. Responses retain their final OK or
// ERROR line so every write can be checked without relying on transport exit.
type ATClient interface {
	Command(context.Context, string, string, time.Duration) (string, error)
}

type USBInterfaces struct {
	VendorID     int
	ProductID    int
	ADB          bool
	UACControl   bool
	UACStreaming bool
}

func (u USBInterfaces) voiceReady() bool {
	return u.ADB && u.UACControl && u.UACStreaming
}

type ATPort struct {
	Device          string
	Interface       string
	SysfsDevicePath string
}

// DeviceAccess resolves one line to its exact sysfs topology and observes the
// USB disconnect/reconnect caused by AT+CFUN=1,1.
type DeviceAccess interface {
	Inspect(string) (USBInterfaces, error)
	ATPort(domain.Line) (ATPort, error)
	WaitForReenumeration(
		context.Context,
		string,
		ATPort,
		int,
		int,
		time.Duration,
	) (ATPort, USBInterfaces, error)
}

type Options struct {
	Inhibitor           Inhibitor
	ATClient            ATClient
	DeviceAccess        DeviceAccess
	ReenumerationWait   time.Duration
	ADBSettleDelay      time.Duration
	PostAuthSettleDelay time.Duration
}

// Manager performs only the persistent USB-composition provisioning needed by
// the resident voice runtime. It never changes usbnet, IMS, or unrelated USB
// function bits.
type Manager struct {
	inhibitor           Inhibitor
	at                  ATClient
	devices             DeviceAccess
	reenumerationWait   time.Duration
	adbSettleDelay      time.Duration
	postAuthSettleDelay time.Duration
}

func New(options Options) (*Manager, error) {
	if options.Inhibitor == nil {
		return nil, fmt.Errorf("QDC507 USB provisioner requires a ModemManager inhibitor")
	}
	at := options.ATClient
	if at == nil {
		at = newSerialATClient()
	}
	devices := options.DeviceAccess
	if devices == nil {
		devices = newSysfsDeviceAccess()
	}
	reenumerationWait := options.ReenumerationWait
	if reenumerationWait <= 0 {
		reenumerationWait = 90 * time.Second
	}
	adbSettleDelay := options.ADBSettleDelay
	if adbSettleDelay <= 0 {
		// DJOneHub observed that an ADB CNXN before this window can leave the
		// MDM9607 gadget transport silent for the remainder of the boot.
		adbSettleDelay = 35 * time.Second
	}
	postAuthSettleDelay := options.PostAuthSettleDelay
	if postAuthSettleDelay <= 0 {
		postAuthSettleDelay = 2 * time.Second
	}
	return &Manager{
		inhibitor:           options.Inhibitor,
		at:                  at,
		devices:             devices,
		reenumerationWait:   reenumerationWait,
		adbSettleDelay:      adbSettleDelay,
		postAuthSettleDelay: postAuthSettleDelay,
	}, nil
}

// Ensure enables ADB and UAC together while preserving VID/PID and the first
// five function bits. A configuration write is followed by an exact readback,
// one controlled module reboot, a second exact readback, and fresh QADBKEY
// authorization. An already-correct live composition is never rebooted.
func (m *Manager) Ensure(ctx context.Context, line domain.Line) (changed bool, err error) {
	if ctx == nil {
		return false, fmt.Errorf("QDC507 USB provisioning context is required")
	}
	if m == nil || m.inhibitor == nil || m.at == nil || m.devices == nil {
		return false, fmt.Errorf("QDC507 USB provisioner is unavailable")
	}
	physicalDevice, err := cleanPhysicalDevice(line.PhysicalDevice)
	if err != nil {
		return false, err
	}
	interfaces, err := m.devices.Inspect(physicalDevice)
	if err != nil {
		return false, fmt.Errorf("inspect QDC507 USB interfaces: %w", err)
	}
	if interfaces.voiceReady() {
		return false, nil
	}
	port, err := m.devices.ATPort(line)
	if err != nil {
		return false, fmt.Errorf("resolve QDC507 AT port: %w", err)
	}
	release, err := m.inhibitor.Inhibit(ctx, physicalDevice)
	if err != nil {
		return false, fmt.Errorf("inhibit QDC507 in ModemManager: %w", err)
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if releaseErr := release(releaseContext); releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("release QDC507 ModemManager inhibition: %w", releaseErr))
		}
	}()

	original, err := m.readComposition(ctx, port.Device)
	if err != nil {
		return false, err
	}
	if interfaces.VendorID != original.VendorID || interfaces.ProductID != original.ProductID {
		return false, fmt.Errorf(
			"QDC507 USBCFG identity %04x:%04x does not match live USB identity %04x:%04x",
			original.VendorID,
			original.ProductID,
			interfaces.VendorID,
			interfaces.ProductID,
		)
	}
	target, err := original.voiceTarget()
	if err != nil {
		return false, err
	}
	if original.equal(target) {
		return false, fmt.Errorf(
			"QDC507 saved USBCFG already enables ADB and UAC, but the live descriptors are incomplete; automatic provisioning will not reboot an unchanged configuration",
		)
	}

	authorized := false
	if !original.hasADB() {
		if err := m.authorizeADB(ctx, port.Device); err != nil {
			return false, err
		}
		authorized = true
	}
	writeResponse, writeErr := m.at.Command(ctx, port.Device, target.command(), 8*time.Second)
	if writeErr != nil || !atResponseSucceeded(writeResponse) {
		return false, fmt.Errorf("QDC507 rejected the preserved ADB/UAC USB composition")
	}
	actual, err := m.readComposition(ctx, port.Device)
	if err != nil {
		return false, fmt.Errorf("read QDC507 USBCFG after write: %w", err)
	}
	if err := validateReadBackBeforeReboot(original, target, actual); err != nil {
		return false, err
	}

	rebootStarted := time.Now()
	rebootResponse, rebootErr := m.at.Command(ctx, port.Device, "AT+CFUN=1,1", 8*time.Second)
	if atResponseIsError(rebootResponse) {
		return false, fmt.Errorf("QDC507 rejected the controlled reboot")
	}
	_, observedInterfaces, waitErr := m.devices.WaitForReenumeration(
		ctx,
		physicalDevice,
		port,
		target.VendorID,
		target.ProductID,
		m.reenumerationWait,
	)
	if waitErr != nil {
		return true, errors.Join(
			fmt.Errorf("wait for QDC507 USB re-enumeration: %w", waitErr),
			rebootErr,
		)
	}
	if !observedInterfaces.voiceReady() {
		return true, fmt.Errorf("QDC507 re-enumerated without complete ADB and UAC descriptors")
	}
	// A QDC507 may expose its new gadget descriptors before the modem-side AT
	// service is stable. Keep ModemManager inhibited through the documented ADB
	// quiet window, then resolve the fresh tty node and verify the final state.
	// This also guarantees that the voice runtime cannot start an early ADB CNXN.
	if err := waitUntil(ctx, rebootStarted.Add(m.adbSettleDelay)); err != nil {
		return true, err
	}
	postInterfaces, err := m.devices.Inspect(physicalDevice)
	if err != nil {
		return true, fmt.Errorf("inspect settled QDC507 USB interfaces: %w", err)
	}
	if postInterfaces.VendorID != target.VendorID || postInterfaces.ProductID != target.ProductID {
		return true, fmt.Errorf(
			"QDC507 settled as unexpected USB identity %04x:%04x",
			postInterfaces.VendorID,
			postInterfaces.ProductID,
		)
	}
	if !postInterfaces.voiceReady() {
		return true, fmt.Errorf("QDC507 settled without complete ADB and UAC descriptors")
	}
	postPort, err := m.devices.ATPort(line)
	if err != nil {
		return true, fmt.Errorf("resolve settled QDC507 AT port: %w", err)
	}
	postComposition, err := m.readComposition(ctx, postPort.Device)
	if err != nil {
		return true, fmt.Errorf("read QDC507 USBCFG after reboot: %w", err)
	}
	if !postComposition.equal(target) {
		return true, fmt.Errorf("QDC507 USBCFG after reboot does not match the preserved ADB/UAC target")
	}
	if authorized {
		if err := m.authorizeADB(ctx, postPort.Device); err != nil {
			return true, fmt.Errorf("authorize QDC507 ADB after reboot: %w", err)
		}
		if err := waitUntil(ctx, time.Now().Add(m.postAuthSettleDelay)); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (m *Manager) readComposition(ctx context.Context, port string) (usbComposition, error) {
	response, err := m.at.Command(ctx, port, `AT+QCFG="USBCFG"`, 5*time.Second)
	if err != nil {
		return usbComposition{}, fmt.Errorf("query QDC507 USBCFG: %w", err)
	}
	if !atResponseSucceeded(response) {
		return usbComposition{}, fmt.Errorf("QDC507 USBCFG query did not return OK")
	}
	composition, err := parseUSBComposition(response)
	if err != nil {
		return usbComposition{}, fmt.Errorf("parse QDC507 USBCFG: %w", err)
	}
	return composition, nil
}

func (m *Manager) authorizeADB(ctx context.Context, port string) error {
	response, err := m.at.Command(ctx, port, "AT+QADBKEY?", 5*time.Second)
	if err != nil || !atResponseSucceeded(response) {
		return fmt.Errorf("QDC507 did not return a supported QADBKEY challenge")
	}
	challenge, err := parseLegacyQADBKeyChallenge(response)
	response = ""
	if err != nil {
		return err
	}
	password, err := legacyQADBUnlockPassword(challenge)
	challenge = ""
	if err != nil {
		return err
	}
	command := fmt.Sprintf(`AT+QADBKEY="%s"`, password)
	authorizationResponse, commandErr := m.at.Command(ctx, port, command, 8*time.Second)
	accepted := commandErr == nil && atResponseSucceeded(authorizationResponse)
	password = ""
	command = ""
	authorizationResponse = ""
	if !accepted {
		return fmt.Errorf("QDC507 rejected the locally derived QADBKEY response")
	}
	return nil
}

type usbComposition struct {
	VendorID  int
	ProductID int
	Flags     []int
}

func (c usbComposition) validate() error {
	if c.VendorID <= 0 || c.VendorID > 0xffff || c.ProductID <= 0 || c.ProductID > 0xffff {
		return fmt.Errorf("QDC507 USBCFG VID/PID is outside the safe range")
	}
	if len(c.Flags) != 7 {
		return fmt.Errorf("QDC507 USBCFG returned %d function bits; expected 7", len(c.Flags))
	}
	for _, flag := range c.Flags {
		if flag != 0 && flag != 1 {
			return fmt.Errorf("QDC507 USBCFG contains a non-boolean function bit")
		}
	}
	return nil
}

func (c usbComposition) voiceTarget() (usbComposition, error) {
	if err := c.validate(); err != nil {
		return usbComposition{}, err
	}
	target := usbComposition{
		VendorID:  c.VendorID,
		ProductID: c.ProductID,
		Flags:     append([]int(nil), c.Flags...),
	}
	target.Flags[len(target.Flags)-2] = 1
	target.Flags[len(target.Flags)-1] = 1
	return target, nil
}

func (c usbComposition) hasADB() bool {
	return len(c.Flags) == 7 && c.Flags[5] == 1
}

func (c usbComposition) equal(other usbComposition) bool {
	if c.VendorID != other.VendorID || c.ProductID != other.ProductID || len(c.Flags) != len(other.Flags) {
		return false
	}
	for index := range c.Flags {
		if c.Flags[index] != other.Flags[index] {
			return false
		}
	}
	return true
}

func (c usbComposition) command() string {
	parts := []string{
		fmt.Sprintf("0x%04X", c.VendorID),
		fmt.Sprintf("0x%04X", c.ProductID),
	}
	for _, flag := range c.Flags {
		parts = append(parts, strconv.Itoa(flag))
	}
	return `AT+QCFG="USBCFG",` + strings.Join(parts, ",")
}

func parseUSBComposition(response string) (usbComposition, error) {
	normalized := strings.ReplaceAll(response, "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		marker := `+qcfg: "usbcfg",`
		index := strings.Index(lower, marker)
		if index < 0 {
			continue
		}
		fields := strings.Split(line[index+len(marker):], ",")
		if len(fields) != 9 {
			return usbComposition{}, fmt.Errorf("QDC507 USBCFG field count is %d; expected 9", len(fields))
		}
		parse := func(raw string) (int, error) {
			value, err := strconv.ParseInt(strings.TrimSpace(raw), 0, 32)
			return int(value), err
		}
		vendorID, err := parse(fields[0])
		if err != nil {
			return usbComposition{}, fmt.Errorf("parse QDC507 USB vendor ID: %w", err)
		}
		productID, err := parse(fields[1])
		if err != nil {
			return usbComposition{}, fmt.Errorf("parse QDC507 USB product ID: %w", err)
		}
		flags := make([]int, 0, 7)
		for _, field := range fields[2:] {
			flag, err := parse(field)
			if err != nil {
				return usbComposition{}, fmt.Errorf("parse QDC507 USB function bit: %w", err)
			}
			flags = append(flags, flag)
		}
		composition := usbComposition{VendorID: vendorID, ProductID: productID, Flags: flags}
		if err := composition.validate(); err != nil {
			return usbComposition{}, err
		}
		return composition, nil
	}
	return usbComposition{}, fmt.Errorf("QDC507 did not return a recognizable USBCFG line")
}

func validateReadBackBeforeReboot(original, target, actual usbComposition) error {
	if err := original.validate(); err != nil {
		return err
	}
	if err := target.validate(); err != nil {
		return err
	}
	if err := actual.validate(); err != nil {
		return err
	}
	if original.VendorID != target.VendorID || original.ProductID != target.ProductID ||
		actual.VendorID != original.VendorID || actual.ProductID != original.ProductID {
		return fmt.Errorf("QDC507 USBCFG readback changed VID/PID unexpectedly")
	}
	for index := range original.Flags {
		if original.Flags[index] == target.Flags[index] && actual.Flags[index] != original.Flags[index] {
			return fmt.Errorf("QDC507 USBCFG readback changed untouched function bit %d", index+1)
		}
		if actual.Flags[index] != original.Flags[index] && actual.Flags[index] != target.Flags[index] {
			return fmt.Errorf("QDC507 USBCFG readback returned an unexpected value for function bit %d", index+1)
		}
	}
	return nil
}

func parseLegacyQADBKeyChallenge(response string) (string, error) {
	if atResponseIsError(response) || !atResponseSucceeded(response) {
		return "", fmt.Errorf("QDC507 does not expose the supported legacy QADBKEY protocol")
	}
	matches := legacyQADBKeyChallengePattern.FindAllStringSubmatch(response, 2)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return "", fmt.Errorf("QDC507 did not return exactly one 8-digit QADBKEY challenge")
	}
	return matches[0][1], nil
}

func legacyQADBUnlockPassword(challenge string) (string, error) {
	if !legacyQADBKeyValuePattern.MatchString(challenge) {
		return "", fmt.Errorf("QADBKEY challenge format is invalid")
	}
	hash := md5Crypt([]byte(legacyQADBKeySecret), []byte(challenge))
	prefix := md5CryptMagic + challenge + "$"
	if !strings.HasPrefix(hash, prefix) || len(hash) < len(prefix)+legacyQADBKeyLength {
		return "", fmt.Errorf("derive QADBKEY response")
	}
	return hash[len(prefix) : len(prefix)+legacyQADBKeyLength], nil
}

func md5Crypt(password, salt []byte) string {
	initial := md5.New() // #nosec G401 -- required by the modem's legacy protocol.
	_, _ = initial.Write(password)
	_, _ = initial.Write([]byte(md5CryptMagic))
	_, _ = initial.Write(salt)

	alternate := md5.New() // #nosec G401 -- required by the modem's legacy protocol.
	_, _ = alternate.Write(password)
	_, _ = alternate.Write(salt)
	_, _ = alternate.Write(password)
	alternateSum := alternate.Sum(nil)
	for remaining := len(password); remaining > 0; remaining -= md5.Size {
		count := remaining
		if count > md5.Size {
			count = md5.Size
		}
		_, _ = initial.Write(alternateSum[:count])
	}
	for count := len(password); count > 0; count >>= 1 {
		if count&1 != 0 {
			_, _ = initial.Write([]byte{0})
		} else {
			_, _ = initial.Write(password[:1])
		}
	}
	digest := initial.Sum(nil)

	for round := 0; round < 1000; round++ {
		current := md5.New() // #nosec G401 -- required by the modem's legacy protocol.
		if round&1 != 0 {
			_, _ = current.Write(password)
		} else {
			_, _ = current.Write(digest)
		}
		if round%3 != 0 {
			_, _ = current.Write(salt)
		}
		if round%7 != 0 {
			_, _ = current.Write(password)
		}
		if round&1 != 0 {
			_, _ = current.Write(digest)
		} else {
			_, _ = current.Write(password)
		}
		digest = current.Sum(nil)
	}

	encoded := strings.Builder{}
	encoded.Grow(22)
	writeMD5CryptBase64(&encoded, digest[0], digest[6], digest[12], 4)
	writeMD5CryptBase64(&encoded, digest[1], digest[7], digest[13], 4)
	writeMD5CryptBase64(&encoded, digest[2], digest[8], digest[14], 4)
	writeMD5CryptBase64(&encoded, digest[3], digest[9], digest[15], 4)
	writeMD5CryptBase64(&encoded, digest[4], digest[10], digest[5], 4)
	writeMD5CryptBase64(&encoded, 0, 0, digest[11], 2)
	return md5CryptMagic + string(salt) + "$" + encoded.String()
}

func writeMD5CryptBase64(output *strings.Builder, high, middle, low byte, count int) {
	value := uint32(high)<<16 | uint32(middle)<<8 | uint32(low)
	for index := 0; index < count; index++ {
		output.WriteByte(md5CryptAlphabet[value&0x3f])
		value >>= 6
	}
}

func atResponseSucceeded(response string) bool {
	return !atResponseIsError(response) && responseHasLine(response, "OK")
}

func atResponseIsError(response string) bool {
	for _, line := range responseLines(response) {
		upper := strings.ToUpper(line)
		if upper == "ERROR" || strings.HasPrefix(upper, "+CME ERROR:") || strings.HasPrefix(upper, "+CMS ERROR:") {
			return true
		}
	}
	return false
}

func responseHasLine(response, wanted string) bool {
	for _, line := range responseLines(response) {
		if strings.EqualFold(line, wanted) {
			return true
		}
	}
	return false
}

func responseLines(response string) []string {
	normalized := strings.ReplaceAll(response, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(normalized, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func waitUntil(ctx context.Context, target time.Time) error {
	delay := time.Until(target)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
