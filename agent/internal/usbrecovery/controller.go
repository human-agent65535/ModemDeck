package usbrecovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	defaultSysDevicesRoot = "/sys/devices"
	defaultUSBDeviceRoot  = "/dev/bus/usb"
	defaultCooldown       = 2 * time.Minute
)

var ErrBusy = errors.New("a USB reset is already in progress")

type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string {
	return e.Reason
}

type CooldownError struct {
	Remaining time.Duration
}

func (e *CooldownError) Error() string {
	return fmt.Sprintf("USB reset is cooling down for %s", e.Remaining.Round(time.Second))
}

type PermissionError struct {
	Cause error
}

func (e *PermissionError) Error() string {
	return "host agent does not have permission to reset this USB device"
}

func (e *PermissionError) Unwrap() error {
	return e.Cause
}

type controllerOptions struct {
	sysDevicesRoot string
	usbDeviceRoot  string
	cooldown       time.Duration
	now            func() time.Time
	stat           func(string) (fs.FileInfo, error)
	writable       func(string) error
	reset          func(context.Context, string) error
}

type Controller struct {
	sysDevicesRoot string
	usbDeviceRoot  string
	cooldown       time.Duration
	now            func() time.Time
	stat           func(string) (fs.FileInfo, error)
	writable       func(string) error
	reset          func(context.Context, string) error

	mu        sync.Mutex
	busy      bool
	lastReset map[string]time.Time
}

type target struct {
	key        string
	deviceNode string
}

func New() *Controller {
	return newController(controllerOptions{})
}

func newController(options controllerOptions) *Controller {
	if strings.TrimSpace(options.sysDevicesRoot) == "" {
		options.sysDevicesRoot = defaultSysDevicesRoot
	}
	if strings.TrimSpace(options.usbDeviceRoot) == "" {
		options.usbDeviceRoot = defaultUSBDeviceRoot
	}
	if options.cooldown <= 0 {
		options.cooldown = defaultCooldown
	}
	if options.now == nil {
		options.now = time.Now
	}
	if options.stat == nil {
		options.stat = os.Lstat
	}
	if options.writable == nil {
		options.writable = deviceWritable
	}
	if options.reset == nil {
		options.reset = resetUSBDevice
	}
	return &Controller{
		sysDevicesRoot: filepath.Clean(options.sysDevicesRoot),
		usbDeviceRoot:  filepath.Clean(options.usbDeviceRoot),
		cooldown:       options.cooldown,
		now:            options.now,
		stat:           options.stat,
		writable:       options.writable,
		reset:          options.reset,
		lastReset:      make(map[string]time.Time),
	}
}

func (c *Controller) Capability(physicalDevice string) domain.FeatureCapability {
	capability := domain.FeatureCapability{
		Backend:     "linux_usbfs",
		Implemented: true,
	}
	resolved, err := c.resolve(physicalDevice)
	if err != nil {
		capability.Reason = unsupportedReason(err)
		return capability
	}
	capability.Supported = true
	capability.Readable = true
	info, err := c.stat(resolved.deviceNode)
	if err != nil {
		capability.Reason = "USB device node is unavailable to the host agent"
		return capability
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		capability.Reason = "resolved USB device node is not a character device"
		return capability
	}
	if err := c.writable(resolved.deviceNode); err != nil {
		capability.Reason = "host agent does not have write access to the USB device node"
		return capability
	}
	capability.Writable = true
	return capability
}

func (c *Controller) Reset(ctx context.Context, physicalDevice string) error {
	if ctx == nil {
		return &UnsupportedError{Reason: "request context is required"}
	}
	resolved, err := c.resolve(physicalDevice)
	if err != nil {
		return &UnsupportedError{Reason: unsupportedReason(err)}
	}
	info, err := c.stat(resolved.deviceNode)
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return &UnsupportedError{Reason: "USB device node is unavailable to the host agent"}
	}
	if err := c.writable(resolved.deviceNode); err != nil {
		return &PermissionError{Cause: err}
	}

	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return ErrBusy
	}
	now := c.now()
	if last := c.lastReset[resolved.key]; !last.IsZero() {
		remaining := c.cooldown - now.Sub(last)
		if remaining > 0 {
			c.mu.Unlock()
			return &CooldownError{Remaining: remaining}
		}
	}
	c.busy = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
	}()
	if err := c.reset(ctx, resolved.deviceNode); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return &PermissionError{Cause: err}
		}
		return err
	}
	c.mu.Lock()
	c.lastReset[resolved.key] = c.now()
	c.mu.Unlock()
	return nil
}

func (c *Controller) resolve(physicalDevice string) (target, error) {
	physicalDevice = filepath.Clean(strings.TrimSpace(physicalDevice))
	if physicalDevice == "." || !filepath.IsAbs(physicalDevice) {
		return target{}, errors.New("ModemManager did not report an absolute physical device path")
	}
	resolvedRoot, err := filepath.EvalSymlinks(c.sysDevicesRoot)
	if err != nil {
		return target{}, errors.New("host sysfs device tree is unavailable")
	}
	resolvedPhysical, err := filepath.EvalSymlinks(physicalDevice)
	if err != nil {
		return target{}, errors.New("ModemManager physical device path is unavailable")
	}
	if !pathWithin(resolvedRoot, resolvedPhysical) {
		return target{}, errors.New("ModemManager physical device path is outside the host sysfs device tree")
	}

	current := resolvedPhysical
	for pathWithin(resolvedRoot, current) && current != resolvedRoot {
		busNumber, busErr := readPositiveDecimal(filepath.Join(current, "busnum"))
		deviceNumber, deviceErr := readPositiveDecimal(filepath.Join(current, "devnum"))
		vendor, vendorErr := readHexIdentifier(filepath.Join(current, "idVendor"))
		product, productErr := readHexIdentifier(filepath.Join(current, "idProduct"))
		if busErr == nil && deviceErr == nil && vendorErr == nil && productErr == nil {
			class, classErr := readHexIdentifier(filepath.Join(current, "bDeviceClass"))
			if classErr != nil {
				return target{}, errors.New("resolved USB device class is unavailable")
			}
			if class == "09" {
				return target{}, errors.New("resolved sysfs object is a USB host controller")
			}
			return target{
				key: current + ":" + vendor + ":" + product,
				deviceNode: filepath.Join(
					c.usbDeviceRoot,
					fmt.Sprintf("%03d", busNumber),
					fmt.Sprintf("%03d", deviceNumber),
				),
			}, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return target{}, errors.New("ModemManager physical device is not backed by a USB device")
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func readPositiveDecimal(path string) (int, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(value)))
	if err != nil || number < 1 || number > 999 {
		return 0, errors.New("invalid USB device number")
	}
	return number, nil
}

func readHexIdentifier(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	identifier := strings.ToLower(strings.TrimSpace(string(value)))
	if len(identifier) != 2 && len(identifier) != 4 {
		return "", errors.New("invalid USB identifier")
	}
	if _, err := strconv.ParseUint(identifier, 16, 16); err != nil {
		return "", errors.New("invalid USB identifier")
	}
	return identifier, nil
}

func unsupportedReason(err error) string {
	if err == nil || strings.TrimSpace(err.Error()) == "" {
		return "USB hard reset is unavailable for this modem"
	}
	return err.Error()
}
