package qdc507voice

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const shellStatusMarker = "__MODEMDECK_QDC507_EXIT__"

type execClient struct {
	executable  string
	environment []string
}

func newExecClient(stateDirectory string) (*execClient, error) {
	executable, err := exec.LookPath("adb")
	if err != nil {
		return nil, fmt.Errorf("locate adb for QDC507 voice runtime: %w", err)
	}
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create QDC507 ADB state directory: %w", err)
	}
	absolute, err := filepath.Abs(stateDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve QDC507 ADB state directory: %w", err)
	}
	return &execClient{
		executable: executable,
		environment: append(
			os.Environ(),
			"HOME="+absolute,
			"ADB_SERVER_SOCKET=localfilesystem:"+filepath.Join(absolute, "server.sock"),
		),
	}, nil
}

func (c *execClient) Devices(ctx context.Context) ([]Device, error) {
	output, err := c.run(ctx, 8*time.Second, "devices", "-l")
	if err != nil {
		return nil, fmt.Errorf("list ADB devices: %w", err)
	}
	return parseDevices(output), nil
}

func (c *execClient) Shell(
	ctx context.Context,
	selector string,
	command string,
	timeout time.Duration,
) (ShellResult, error) {
	target, err := adbTargetArguments(selector)
	if err != nil {
		return ShellResult{}, err
	}
	wrapped := "( " + command + " ); rc=$?; printf '\\n" + shellStatusMarker + "%s\\n' \"$rc\""
	arguments := append(target, "shell", wrapped)
	output, runErr := c.run(ctx, timeout, arguments...)
	marker := strings.LastIndex(output, shellStatusMarker)
	if marker < 0 {
		if runErr != nil {
			return ShellResult{}, runErr
		}
		return ShellResult{}, fmt.Errorf("ADB shell response did not contain an exit status")
	}
	statusText := strings.TrimSpace(output[marker+len(shellStatusMarker):])
	statusFields := strings.Fields(statusText)
	if len(statusFields) == 0 {
		return ShellResult{}, fmt.Errorf("ADB shell exit status was empty")
	}
	status, err := strconv.Atoi(statusFields[0])
	if err != nil || status < 0 || status > 255 {
		return ShellResult{}, fmt.Errorf("ADB shell exit status was invalid")
	}
	result := ShellResult{
		Output: strings.TrimSpace(output[:marker]),
		Status: status,
	}
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

func (c *execClient) Push(
	ctx context.Context,
	selector string,
	local string,
	remote string,
	mode fs.FileMode,
	timeout time.Duration,
) error {
	target, err := adbTargetArguments(selector)
	if err != nil {
		return err
	}
	arguments := append(target, "push", local, remote)
	if _, err := c.run(ctx, timeout, arguments...); err != nil {
		return err
	}
	result, err := c.Shell(
		ctx,
		selector,
		fmt.Sprintf("chmod %04o '%s'", mode.Perm(), remote),
		8*time.Second,
	)
	if err != nil {
		return err
	}
	if result.Status != 0 {
		return fmt.Errorf("chmod remote runtime file returned status %d", result.Status)
	}
	return nil
}

func (c *execClient) run(
	ctx context.Context,
	timeout time.Duration,
	arguments ...string,
) (string, error) {
	if c == nil || c.executable == "" {
		return "", fmt.Errorf("ADB client is unavailable")
	}
	if ctx == nil {
		return "", fmt.Errorf("ADB command context is required")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(bounded, c.executable, arguments...)
	command.Env = c.environment
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if bounded.Err() != nil {
		return text, bounded.Err()
	}
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w: %s", err, compactCommandOutput(text))
	}
	return text, nil
}

func parseDevices(output string) []Device {
	var devices []Device
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		serial := ""
		attributes := []string(nil)
		device := Device{}
		if strings.HasPrefix(line, "(no serial number)") {
			fields = strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "(no serial number)")))
			if len(fields) < 1 {
				continue
			}
			device.State = fields[0]
			attributes = fields[1:]
		} else {
			if len(fields) < 2 {
				continue
			}
			serial = fields[0]
			device.State = fields[1]
			attributes = fields[2:]
		}
		transportID := ""
		for _, field := range attributes {
			if strings.HasPrefix(field, "usb:") {
				device.USB = field
			}
			if strings.HasPrefix(field, "transport_id:") {
				transportID = strings.TrimPrefix(field, "transport_id:")
			}
		}
		if id, err := strconv.Atoi(transportID); err == nil && id > 0 {
			device.Selector = "transport:" + strconv.Itoa(id)
		} else if safeSerial(serial) {
			device.Selector = "serial:" + serial
		} else {
			continue
		}
		devices = append(devices, device)
	}
	return devices
}

func adbTargetArguments(selector string) ([]string, error) {
	if value, found := strings.CutPrefix(selector, "transport:"); found {
		id, err := strconv.Atoi(value)
		if err == nil && id > 0 && strconv.Itoa(id) == value {
			return []string{"-t", value}, nil
		}
		return nil, fmt.Errorf("ADB transport selector is invalid")
	}
	if value, found := strings.CutPrefix(selector, "serial:"); found && safeSerial(value) {
		return []string{"-s", value}, nil
	}
	return nil, fmt.Errorf("ADB device selector is invalid")
}

func safeSerial(serial string) bool {
	if serial == "" {
		return false
	}
	for _, character := range serial {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func compactCommandOutput(output string) string {
	output = strings.Join(strings.Fields(output), " ")
	if len(output) > 500 {
		return output[len(output)-500:]
	}
	return output
}

var _ Client = (*execClient)(nil)
