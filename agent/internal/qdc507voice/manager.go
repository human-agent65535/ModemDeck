package qdc507voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	runtimeVersion  = "qdc507-3.18.44-voice-20260712.5"
	remoteDirectory = "/tmp/modemdeck-qdc507-voice"
	routePIDFile    = "/run/modemdeck-qdc507-voice-route.pid"
	routeLogFile    = "/run/modemdeck-qdc507-voice-route.log"
	calibrationPID  = "/run/modemdeck-qdc507-alsaucm.pid"
	calibrationLog  = "/run/modemdeck-qdc507-alsaucm.log"
)

type manifest struct {
	FormatVersion   int            `json:"formatVersion"`
	RuntimeVersion  string         `json:"runtimeVersion"`
	KernelRelease   string         `json:"kernelRelease"`
	CardName        string         `json:"cardName"`
	Helper          string         `json:"helper"`
	Files           []manifestFile `json:"files"`
	Modules         []module       `json:"modules"`
	RequiredDevices []string       `json:"requiredDevices"`
}

type manifestFile struct {
	Name string `json:"name"`
	Mode uint32 `json:"mode"`
}

type module struct {
	File string `json:"file"`
	Name string `json:"name"`
}

var expectedManifest = manifest{
	FormatVersion:  1,
	RuntimeVersion: runtimeVersion,
	KernelRelease:  "3.18.44",
	CardName:       "mdm9607-tomtom-i2s-snd-card",
	Helper:         "mavo-pcm-bridge.armv7",
	Files: []manifestFile{
		{Name: "qdc507_aprv3.ko", Mode: 0o644},
		{Name: "qdc507_voice.ko", Mode: 0o644},
		{Name: "mavo-pcm-bridge.armv7", Mode: 0o755},
	},
	Modules: []module{
		{File: "qdc507_aprv3.ko", Name: "qdc507_aprv3"},
		{File: "qdc507_voice.ko", Name: "qdc507_voice"},
	},
	RequiredDevices: []string{
		"/dev/snd/controlC0",
		"/dev/snd/pcmC0D4p",
		"/dev/snd/pcmC0D4c",
		"/dev/snd/pcmC0D5p",
		"/dev/snd/pcmC0D6c",
	},
}

type expectedArtifact struct {
	size   int64
	sha256 string
}

var expectedArtifacts = map[string]expectedArtifact{
	"manifest.json": {
		size:   729,
		sha256: "f4f6c266ced7015d4e61d993a6e31247c26a9e85a8fdf1c6d842c459e1e2970a",
	},
	"qdc507_aprv3.ko": {
		size:   36664,
		sha256: "3d82d3dec4f1e323201bba87156df9d41438e08314097353f2607f9117211d4a",
	},
	"qdc507_voice.ko": {
		size:   999236,
		sha256: "ed3821682d5309969a01c764192c83feff9669c61ef237c69475cd1619cf296c",
	},
	"mavo-pcm-bridge.armv7": {
		size:   17860,
		sha256: "88d47c15e61d1428a59c821fed804c2e6490e82859a085062f21966b58d167fc",
	},
}

type Device struct {
	Selector string
	State    string
	USB      string
}

type ShellResult struct {
	Output string
	Status int
}

// Client is the narrow ADB boundary used by the module reconciler. The
// production implementation invokes the container-local Android platform
// tools; tests supply a deterministic fake.
type Client interface {
	Devices(context.Context) ([]Device, error)
	Shell(context.Context, string, string, time.Duration) (ShellResult, error)
	Push(context.Context, string, string, string, fs.FileMode, time.Duration) error
}

type Options struct {
	RuntimeDirectory string
	Client           Client
	DeviceWait       time.Duration
	PollInterval     time.Duration
}

type lineState struct {
	status   domain.QDC507VoiceRuntimeStatus
	bootID   string
	selector string
}

type Manager struct {
	runtimeDirectory string
	manifest         manifest
	client           Client
	deviceWait       time.Duration
	pollInterval     time.Duration

	reconcileMu sync.Mutex
	mu          sync.RWMutex
	states      map[string]lineState
}

func New(options Options) (*Manager, error) {
	directory := strings.TrimSpace(options.RuntimeDirectory)
	manager := &Manager{states: make(map[string]lineState)}
	if directory == "" {
		return manager, nil
	}
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("QDC507 voice runtime directory must be absolute")
	}
	decoded, err := validateRuntimeDirectory(directory)
	if err != nil {
		return nil, err
	}
	client := options.Client
	if client == nil {
		stateDirectory := filepath.Join(filepath.Dir(directory), "qdc507-adb")
		client, err = newExecClient(stateDirectory)
		if err != nil {
			return nil, err
		}
	}
	deviceWait := options.DeviceWait
	if deviceWait <= 0 {
		deviceWait = 45 * time.Second
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	manager.runtimeDirectory = directory
	manager.manifest = decoded
	manager.client = client
	manager.deviceWait = deviceWait
	manager.pollInterval = pollInterval
	return manager, nil
}

func (m *Manager) Enabled() bool {
	return m != nil && m.runtimeDirectory != ""
}

func (m *Manager) Status(line domain.Line) domain.QDC507VoiceRuntimeStatus {
	if !m.Enabled() || !isQDC507(line) {
		return domain.QDC507VoiceRuntimeStatus{}
	}
	key := physicalDeviceKey(line.PhysicalDevice)
	if key == "" {
		return domain.QDC507VoiceRuntimeStatus{
			Configured:     true,
			RuntimeVersion: m.manifest.RuntimeVersion,
			Reason:         "QDC507 physical USB path is unavailable",
		}
	}
	m.mu.RLock()
	state, found := m.states[key]
	m.mu.RUnlock()
	if found {
		return state.status
	}
	return domain.QDC507VoiceRuntimeStatus{
		Configured:     true,
		RuntimeVersion: m.manifest.RuntimeVersion,
		Reason:         "QDC507 module voice route has not been checked",
	}
}

// Reconcile checks each currently discovered QDC507 once for this lifecycle
// boundary. A ready route is adopted by boot ID; it is never stopped here or
// when a call ends.
func (m *Manager) Reconcile(ctx context.Context, lines []domain.Line) (bool, error) {
	if !m.Enabled() {
		return false, nil
	}
	if ctx == nil {
		return false, fmt.Errorf("QDC507 voice reconcile context is required")
	}
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()

	desired := make(map[string]domain.Line)
	for _, line := range lines {
		if !isQDC507(line) {
			continue
		}
		key := physicalDeviceKey(line.PhysicalDevice)
		if key != "" {
			desired[key] = line
		}
	}

	changed := false
	m.mu.Lock()
	for key := range m.states {
		if _, found := desired[key]; !found {
			delete(m.states, key)
			changed = true
		}
	}
	m.mu.Unlock()

	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var failures []error
	for _, key := range keys {
		line := desired[key]
		previous, _ := m.state(key)
		next, err := m.ensureLine(ctx, line, previous)
		if err != nil {
			next.status = domain.QDC507VoiceRuntimeStatus{
				Configured:     true,
				RuntimeVersion: m.manifest.RuntimeVersion,
				Reason:         compactReason(err),
			}
			failures = append(failures, fmt.Errorf("%s: %w", line.ID, err))
		}
		if m.storeState(key, next) {
			changed = true
		}
		if next.status.Ready {
			slog.Info(
				"QDC507 module voice route ready",
				"component", "qdc507voice",
				"line_id", line.ID,
				"physical_device", key,
				"runtime_version", next.status.RuntimeVersion,
			)
		}
	}
	return changed, errors.Join(failures...)
}

func (m *Manager) ensureLine(
	ctx context.Context,
	line domain.Line,
	previous lineState,
) (lineState, error) {
	device, err := m.waitForDevice(ctx, line.PhysicalDevice)
	if err != nil {
		return lineState{}, err
	}
	root, err := m.client.Shell(ctx, device.Selector, "id -u", 8*time.Second)
	if err != nil {
		return lineState{selector: device.Selector}, fmt.Errorf("query QDC507 ADB identity: %w", err)
	}
	if root.Status != 0 || !containsField(root.Output, "0") {
		return lineState{selector: device.Selector}, fmt.Errorf("QDC507 ADB control channel is not root")
	}
	release, err := m.client.Shell(ctx, device.Selector, "uname -r", 8*time.Second)
	if err != nil {
		return lineState{selector: device.Selector}, fmt.Errorf("query QDC507 kernel release: %w", err)
	}
	if release.Status != 0 || !containsField(release.Output, m.manifest.KernelRelease) {
		return lineState{selector: device.Selector}, fmt.Errorf(
			"QDC507 kernel release does not match runtime %s",
			m.manifest.KernelRelease,
		)
	}
	boot, err := m.client.Shell(
		ctx,
		device.Selector,
		"cat /proc/sys/kernel/random/boot_id",
		8*time.Second,
	)
	if err != nil || boot.Status != 0 || strings.TrimSpace(boot.Output) == "" {
		if err == nil {
			err = fmt.Errorf("remote command returned status %d", boot.Status)
		}
		return lineState{selector: device.Selector}, fmt.Errorf("query QDC507 boot ID: %w", err)
	}
	bootID := strings.TrimSpace(boot.Output)
	state := lineState{bootID: bootID, selector: device.Selector}
	ready, readyErr := m.routeReady(ctx, device.Selector)
	if readyErr == nil && ready {
		state.status = m.readyStatus()
		return state, nil
	}
	if previous.bootID == bootID && previous.status.Ready && readyErr != nil {
		return state, fmt.Errorf("recheck resident QDC507 voice route: %w", readyErr)
	}
	if err := m.prepare(ctx, device.Selector); err != nil {
		return state, err
	}
	if err := m.startRoute(ctx, line.PhysicalDevice, device.Selector); err != nil {
		return state, err
	}
	state.status = m.readyStatus()
	return state, nil
}

func (m *Manager) prepare(ctx context.Context, serial string) error {
	if result, err := m.client.Shell(
		ctx,
		serial,
		"mkdir -p '"+remoteDirectory+"' && chmod 700 '"+remoteDirectory+"'",
		8*time.Second,
	); err != nil || result.Status != 0 {
		return shellFailure("create QDC507 runtime directory", result, err)
	}
	for _, entry := range m.manifest.Files {
		local := filepath.Join(m.runtimeDirectory, entry.Name)
		remote := remoteDirectory + "/" + entry.Name
		if err := m.client.Push(
			ctx,
			serial,
			local,
			remote,
			fs.FileMode(entry.Mode),
			30*time.Second,
		); err != nil {
			return fmt.Errorf("push QDC507 runtime file %s: %w", entry.Name, err)
		}
	}

	ready, err := m.soundDevicesReady(ctx, serial)
	if err != nil {
		return err
	}
	if !ready {
		legacy, legacyErr := m.client.Shell(
			ctx,
			serial,
			"grep -q '^qdc507_afe ' /proc/modules",
			8*time.Second,
		)
		if legacyErr != nil {
			return fmt.Errorf("check legacy QDC507 driver: %w", legacyErr)
		}
		if legacy.Status == 0 {
			return fmt.Errorf("legacy qdc507_afe is resident; reboot the module before loading the supported runtime")
		}
		if legacy.Status != 1 {
			return fmt.Errorf("check legacy QDC507 driver: status %d", legacy.Status)
		}
		for _, module := range m.manifest.Modules {
			present, presentErr := m.client.Shell(
				ctx,
				serial,
				"grep -q '^"+module.Name+" ' /proc/modules",
				8*time.Second,
			)
			if presentErr != nil {
				return fmt.Errorf("check QDC507 kernel module %s: %w", module.Name, presentErr)
			}
			if present.Status == 0 {
				if module.Name == "qdc507_voice" {
					return fmt.Errorf("qdc507_voice is resident without its sound devices; reboot the module before retrying")
				}
				continue
			}
			if present.Status != 1 {
				return fmt.Errorf("check QDC507 kernel module %s: status %d", module.Name, present.Status)
			}
			loaded, loadErr := m.client.Shell(
				ctx,
				serial,
				"insmod '"+remoteDirectory+"/"+module.File+"'",
				20*time.Second,
			)
			if loadErr != nil || loaded.Status != 0 {
				detail, _, _ := m.shellOutput(ctx, serial, "dmesg | tail -n 80", 8*time.Second)
				return shellFailure(
					"load QDC507 kernel module "+module.Name+suffixDetail(detail),
					loaded,
					loadErr,
				)
			}
		}
	}
	wait := "ready=0; n=0; while test \"$n\" -lt 100; do " +
		"if " + m.soundDeviceChecks() + "; then ready=1; break; fi; " +
		"sleep 0.2; n=$((n+1)); done; test \"$ready\" -eq 1"
	if result, err := m.client.Shell(ctx, serial, wait, 25*time.Second); err != nil || result.Status != 0 {
		detail, _, _ := m.shellOutput(ctx, serial, "dmesg | tail -n 80", 8*time.Second)
		return shellFailure("wait for QDC507 sound devices"+suffixDetail(detail), result, err)
	}
	if err := m.ensureCalibration(ctx, serial); err != nil {
		return err
	}
	if result, err := m.client.Shell(
		ctx,
		serial,
		"test -c /dev/ttyGS0 && test -p /run/voc_svr",
		8*time.Second,
	); err != nil || result.Status != 0 {
		return shellFailure("verify QDC507 voice endpoints", result, err)
	}
	helper := remoteDirectory + "/" + m.manifest.Helper
	if result, err := m.client.Shell(
		ctx,
		serial,
		"'"+helper+"' --check",
		15*time.Second,
	); err != nil || result.Status != 0 {
		return shellFailure("run QDC507 PCM bridge self-check", result, err)
	}
	return nil
}

func (m *Manager) ensureCalibration(ctx context.Context, serial string) error {
	command := "owned=0; " +
		"if test -s '" + calibrationPID + "'; then " +
		"read pid expected_start < '" + calibrationPID + "' || true; " +
		"current_start=$(cut -d ' ' -f 22 \"/proc/$pid/stat\" 2>/dev/null); " +
		"argv0=$(tr '\\000' '\\n' < \"/proc/$pid/cmdline\" 2>/dev/null | sed -n '1p'); " +
		"test \"$current_start\" = \"$expected_start\" && " +
		"test \"$argv0\" = /usr/bin/alsaucm_test && owned=1 || true; fi; " +
		"if test \"$owned\" -eq 0; then " +
		"for proc in /proc/[0-9]*; do test -r \"$proc/cmdline\" || continue; " +
		"argv0=$(tr '\\000' '\\n' < \"$proc/cmdline\" 2>/dev/null | sed -n '1p'); " +
		"test \"$argv0\" = /usr/bin/alsaucm_test || continue; oldpid=${proc##*/}; " +
		"kill -TERM \"$oldpid\" 2>/dev/null || true; n=0; " +
		"while kill -0 \"$oldpid\" 2>/dev/null && test \"$n\" -lt 30; do " +
		"sleep 0.1; n=$((n+1)); done; kill -0 \"$oldpid\" 2>/dev/null && exit 71 || true; done; " +
		"rm -f /run/alsaucm_test '" + calibrationPID + "' '" + calibrationLog + "'; " +
		"nohup /usr/bin/alsaucm_test </dev/null >> '" + calibrationLog + "' 2>&1 & pid=$!; " +
		"starttime=$(cut -d ' ' -f 22 \"/proc/$pid/stat\" 2>/dev/null); " +
		"printf '%s %s\\n' \"$pid\" \"$starttime\" > '" + calibrationPID + "'; n=0; " +
		"while test \"$n\" -lt 50 && test ! -p /run/alsaucm_test; do " +
		"kill -0 \"$pid\" 2>/dev/null || exit 72; sleep 0.1; n=$((n+1)); done; " +
		"test -p /run/alsaucm_test || exit 73; fi; " +
		"if ! grep -q 'ACDB -> Sent VocProc Cal!' '" + calibrationLog + "' 2>/dev/null; then " +
		"printf 'open snd_soc_msm_9x07_Tomtom_I2S\\n' > /run/alsaucm_test; " +
		"printf 'set _verb VoLTE\\n' > /run/alsaucm_test; " +
		"printf 'set _enadev Auxpcm Rx\\n' > /run/alsaucm_test; " +
		"printf 'set _enadev Auxpcm Tx\\n' > /run/alsaucm_test; n=0; " +
		"while test \"$n\" -lt 100; do grep -q 'ACDB -> Sent VocProc Cal!' '" + calibrationLog +
		"' 2>/dev/null && break; sleep 0.1; n=$((n+1)); done; fi; " +
		"grep -q 'ACDB -> Sent VocProc Cal!' '" + calibrationLog + "'"
	result, err := m.client.Shell(ctx, serial, command, 25*time.Second)
	if err == nil && result.Status == 0 {
		return nil
	}
	detail, _, _ := m.shellOutput(
		ctx,
		serial,
		"test ! -f '"+calibrationLog+"' || tail -n 100 '"+calibrationLog+"'",
		8*time.Second,
	)
	return shellFailure("initialize QDC507 VoLTE calibration"+suffixDetail(detail), result, err)
}

func (m *Manager) startRoute(ctx context.Context, physicalDevice, serial string) error {
	helper := remoteDirectory + "/" + m.manifest.Helper
	command := "rm -f '" + routePIDFile + "' '" + routeLogFile + "'; " +
		"nohup '" + helper + "' --voice-route-session --verbose " +
		"</dev/null >> '" + routeLogFile + "' 2>&1 & pid=$!; " +
		"starttime=$(cut -d ' ' -f 22 \"/proc/$pid/stat\" 2>/dev/null); " +
		"case \"$pid:$starttime\" in :*|*:|*[!0-9:]*) false;; *) " +
		"printf '%s %s\\n' \"$pid\" \"$starttime\" > '" + routePIDFile + "';; esac"
	launch, launchErr := m.client.Shell(ctx, serial, command, 8*time.Second)
	if launchErr == nil && launch.Status != 0 {
		launchErr = fmt.Errorf("remote command returned status %d", launch.Status)
	}

	deadline := time.NewTimer(m.deviceWait)
	defer deadline.Stop()
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		device, err := m.findDevice(ctx, physicalDevice)
		if err == nil {
			ready, readyErr := m.routeReady(ctx, device.Selector)
			if readyErr == nil && ready {
				return nil
			}
			lastErr = readyErr
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			detail := ""
			if device, err := m.findDevice(ctx, physicalDevice); err == nil {
				detail, _, _ = m.shellOutput(
					ctx,
					device.Selector,
					"test ! -f '"+routeLogFile+"' || tail -n 160 '"+routeLogFile+"'",
					8*time.Second,
				)
			}
			causes := errors.Join(launchErr, lastErr)
			if causes == nil {
				causes = fmt.Errorf("route readiness check remained inactive")
			}
			return fmt.Errorf(
				"QDC507 D4/UAC route did not become ready%s: %w",
				suffixDetail(detail),
				causes,
			)
		case <-ticker.C:
		}
	}
}

func (m *Manager) routeReady(ctx context.Context, serial string) (bool, error) {
	helper := remoteDirectory + "/" + m.manifest.Helper
	command := "test -s '" + routePIDFile + "' && " +
		"read pid expected_start < '" + routePIDFile + "' && " +
		"test \"$(cut -d ' ' -f 22 \"/proc/$pid/stat\" 2>/dev/null)\" = \"$expected_start\" && " +
		"test \"$(tr '\\000' '\\n' < \"/proc/$pid/cmdline\" 2>/dev/null | sed -n '1p')\" = '" + helper + "' && " +
		"tr '\\000' '\\n' < \"/proc/$pid/cmdline\" 2>/dev/null | grep -q '^--voice-route-session$' && " +
		"grep -q 'VoLTE route session active on hw:0,4' '" + routeLogFile + "' && " +
		"test \"$(cat /sys/class/android_usb/f_audio/audio_enable)\" = 1 && " +
		"grep -q '^state: RUNNING' /proc/asound/card0/pcm4p/sub0/status && " +
		"grep -q '^state: RUNNING' /proc/asound/card0/pcm4c/sub0/status"
	result, err := m.client.Shell(ctx, serial, command, 8*time.Second)
	if err != nil {
		return false, err
	}
	return result.Status == 0, nil
}

func (m *Manager) soundDevicesReady(ctx context.Context, serial string) (bool, error) {
	result, err := m.client.Shell(ctx, serial, m.soundDeviceChecks(), 8*time.Second)
	if err != nil {
		return false, fmt.Errorf("check QDC507 sound devices: %w", err)
	}
	return result.Status == 0, nil
}

func (m *Manager) soundDeviceChecks() string {
	checks := make([]string, 0, len(m.manifest.RequiredDevices)+1)
	for _, device := range m.manifest.RequiredDevices {
		checks = append(checks, "test -c '"+device+"'")
	}
	checks = append(checks, "grep -Fq '"+m.manifest.CardName+"' /proc/asound/cards")
	return strings.Join(checks, " && ")
}

func (m *Manager) waitForDevice(ctx context.Context, physicalDevice string) (Device, error) {
	deadline := time.NewTimer(m.deviceWait)
	defer deadline.Stop()
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		device, err := m.findDevice(ctx, physicalDevice)
		if err == nil {
			return device, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return Device{}, ctx.Err()
		case <-deadline.C:
			return Device{}, fmt.Errorf("wait for QDC507 ADB device: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func (m *Manager) findDevice(ctx context.Context, physicalDevice string) (Device, error) {
	usbPort := path.Base(physicalDeviceKey(physicalDevice))
	if usbPort == "" || usbPort == "." || usbPort == "/" {
		return Device{}, fmt.Errorf("QDC507 physical USB port is unavailable")
	}
	devices, err := m.client.Devices(ctx)
	if err != nil {
		return Device{}, err
	}
	want := "usb:" + usbPort
	var matched []Device
	for _, device := range devices {
		if device.USB == want {
			matched = append(matched, device)
		}
	}
	if len(matched) != 1 {
		return Device{}, fmt.Errorf("expected one ADB transport for %s, found %d", want, len(matched))
	}
	if matched[0].State != "device" {
		return Device{}, fmt.Errorf("ADB transport %s is %s", want, matched[0].State)
	}
	return matched[0], nil
}

func (m *Manager) readyStatus() domain.QDC507VoiceRuntimeStatus {
	return domain.QDC507VoiceRuntimeStatus{
		Configured:     true,
		Ready:          true,
		RuntimeVersion: m.manifest.RuntimeVersion,
	}
}

func (m *Manager) state(key string) (lineState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, found := m.states[key]
	return state, found
}

func (m *Manager) storeState(key string, state lineState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, found := m.states[key]
	m.states[key] = state
	return !found || !reflect.DeepEqual(previous, state)
}

func validateRuntimeDirectory(directory string) (manifest, error) {
	for name, expected := range expectedArtifacts {
		artifactPath := filepath.Join(directory, name)
		info, err := os.Lstat(artifactPath)
		if err != nil {
			return manifest{}, fmt.Errorf("read QDC507 voice runtime %s: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() != expected.size {
			return manifest{}, fmt.Errorf("QDC507 voice runtime %s has unexpected type or size", name)
		}
		digest, err := fileSHA256(artifactPath)
		if err != nil {
			return manifest{}, fmt.Errorf("hash QDC507 voice runtime %s: %w", name, err)
		}
		if digest != expected.sha256 {
			return manifest{}, fmt.Errorf("QDC507 voice runtime %s failed SHA-256 verification", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return manifest{}, fmt.Errorf("read QDC507 voice runtime manifest: %w", err)
	}
	var decoded manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		return manifest{}, fmt.Errorf("decode QDC507 voice runtime manifest: %w", err)
	}
	if !reflect.DeepEqual(decoded, expectedManifest) {
		return manifest{}, fmt.Errorf("QDC507 voice runtime manifest is not the reviewed %s release", runtimeVersion)
	}
	return decoded, nil
}

func fileSHA256(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isQDC507(line domain.Line) bool {
	revision := strings.ToUpper(strings.TrimSpace(line.Revision))
	model := strings.ToUpper(strings.TrimSpace(line.Model))
	return strings.HasPrefix(revision, "QDC507") || model == "QDC507"
}

func physicalDeviceKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if !strings.HasPrefix(cleaned, "/sys/devices/") {
		return ""
	}
	return cleaned
}

func containsField(output, wanted string) bool {
	for _, field := range strings.Fields(output) {
		if field == wanted {
			return true
		}
	}
	return false
}

func (m *Manager) shellOutput(
	ctx context.Context,
	serial string,
	command string,
	timeout time.Duration,
) (string, int, error) {
	result, err := m.client.Shell(ctx, serial, command, timeout)
	return strings.TrimSpace(result.Output), result.Status, err
}

func shellFailure(action string, result ShellResult, err error) error {
	detail := strings.TrimSpace(result.Output)
	if len(detail) > 800 {
		detail = detail[len(detail)-800:]
	}
	parts := []string{action}
	if result.Status != 0 {
		parts = append(parts, fmt.Sprintf("status %d", result.Status))
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	message := strings.Join(parts, ": ")
	if err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return errors.New(message)
}

func suffixDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	if len(detail) > 800 {
		detail = detail[len(detail)-800:]
	}
	return ": " + detail
}

func compactReason(err error) string {
	if err == nil {
		return ""
	}
	reason := strings.Join(strings.Fields(err.Error()), " ")
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return reason
}
