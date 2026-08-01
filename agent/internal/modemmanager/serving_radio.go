package modemmanager

import (
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	quectelNetworkInfoQuery     = "AT+QNWINFO"
	servingRadioReadTimeout     = 1500 * time.Millisecond
	servingRadioRefreshInterval = time.Minute
	servingRadioMaximumStaleAge = 5 * time.Minute
	servingRadioSource          = "quectel-qnwinfo"
)

var errServingRadioResponse = errors.New("serving radio response was malformed")

type servingRadioTarget struct {
	index  int
	lineID string
	path   dbus.ObjectPath
}

type servingRadioResult struct {
	index  int
	lineID string
	radio  *domain.ServingRadio
	err    error
}

type servingRadioState struct {
	radio       *domain.ServingRadio
	observedAt  time.Time
	nextAttempt time.Time
}

// projectServingRadios supplements ModemManager's access-technology mask with
// current serving-band telemetry. Queries are bounded, read-only, and only run
// for a registered LTE line from a modem family with a documented QNWINFO
// response. Failure leaves the optional telemetry absent.
func (p *Provider) projectServingRadios(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	if p == nil || parsed == nil || len(parsed.Lines) == 0 {
		return
	}
	hasEligibleLine := false
	for _, line := range parsed.Lines {
		if supportsQuectelServingRadio(line) {
			hasEligibleLine = true
			break
		}
	}
	if !hasEligibleLine {
		p.servingRadioMu.Lock()
		for _, line := range parsed.Lines {
			delete(p.servingRadioStates, line.ID)
		}
		p.servingRadioMu.Unlock()
		return
	}
	now := p.now().UTC()
	targets := make([]servingRadioTarget, 0, len(parsed.Lines))
	p.servingRadioMu.Lock()
	for index := range parsed.Lines {
		line := parsed.Lines[index]
		if !supportsQuectelServingRadio(line) {
			delete(p.servingRadioStates, line.ID)
			continue
		}
		state := p.servingRadioStates[line.ID]
		if state.radio != nil && now.Sub(state.observedAt) <= servingRadioMaximumStaleAge {
			parsed.Lines[index].ServingRadio = cloneServingRadio(state.radio)
		}
		if lineHasCall(parsed.Calls, line.ID, "") || now.Before(state.nextAttempt) {
			continue
		}
		path, found := parsed.LinePaths[line.ID]
		if !found || !path.IsValid() {
			continue
		}
		state.nextAttempt = now.Add(servingRadioRefreshInterval)
		p.servingRadioStates[line.ID] = state
		targets = append(targets, servingRadioTarget{
			index:  index,
			lineID: line.ID,
			path:   path,
		})
	}
	p.servingRadioMu.Unlock()
	if len(targets) == 0 {
		return
	}

	bounded, cancel := context.WithTimeout(ctx, servingRadioReadTimeout)
	defer cancel()
	results := make(chan servingRadioResult, len(targets))
	for _, target := range targets {
		target := target
		go func() {
			response, err := p.commandATPath(
				bounded,
				target.path,
				operation,
				quectelNetworkInfoQuery,
			)
			var radio *domain.ServingRadio
			if err == nil {
				radio, err = parseQuectelNetworkInfo(response)
			}
			results <- servingRadioResult{
				index:  target.index,
				lineID: target.lineID,
				radio:  radio,
				err:    err,
			}
		}()
	}

	for remaining := len(targets); remaining > 0; remaining-- {
		select {
		case result := <-results:
			if result.err == nil {
				parsed.Lines[result.index].ServingRadio = cloneServingRadio(result.radio)
				p.servingRadioMu.Lock()
				state := p.servingRadioStates[result.lineID]
				state.radio = cloneServingRadio(result.radio)
				state.observedAt = now
				p.servingRadioStates[result.lineID] = state
				p.servingRadioMu.Unlock()
			}
		case <-bounded.Done():
			return
		}
	}
}

func supportsQuectelServingRadio(line domain.Line) bool {
	if !line.AccessTechnologiesKnown || line.AccessTechnologies&accessTechnologyLTE == 0 {
		return false
	}
	if !line.RegistrationStateKnown || !registrationStateHasServingNetwork(line.RegistrationStateCode) {
		return false
	}
	identity := strings.ToUpper(strings.Join([]string{
		line.Manufacturer,
		line.Model,
		line.Revision,
		line.HardwareRevision,
	}, " "))
	for _, token := range []string{
		"EC20",
		"EC21",
		"EC25",
		"EG21",
		"EG25",
		"EG91",
		"EG95",
		"EM05",
		"QDC507",
	} {
		if strings.Contains(identity, token) {
			return true
		}
	}
	return false
}

func registrationStateHasServingNetwork(code uint32) bool {
	switch code {
	case 1, 5, 6, 7, 9, 10, 11:
		return true
	default:
		return false
	}
}

func parseQuectelNetworkInfo(response string) (*domain.ServingRadio, error) {
	payload := ""
	for _, line := range strings.Split(strings.ReplaceAll(response, "\r", ""), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "+QNWINFO:") {
			payload = strings.TrimSpace(trimmed[len("+QNWINFO:"):])
			break
		}
	}
	if payload == "" {
		if strings.Contains(strings.ToUpper(response), "NO SERVICE") {
			return nil, nil
		}
		return nil, errServingRadioResponse
	}
	if strings.Contains(strings.ToUpper(payload), "NO SERVICE") {
		return nil, nil
	}

	reader := csv.NewReader(strings.NewReader(payload))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	fields, err := reader.Read()
	if err != nil || len(fields) < 4 {
		return nil, errServingRadioResponse
	}
	mode := strings.ToUpper(strings.TrimSpace(fields[0]))
	radio := &domain.ServingRadio{Source: servingRadioSource}
	switch {
	case strings.Contains(mode, "LTE"):
		radio.AccessTechnology = "lte"
		radio.ChannelType = "earfcn"
	case strings.Contains(mode, "NR5G") || strings.Contains(mode, "5G"):
		radio.AccessTechnology = "nr5g"
		radio.ChannelType = "nrarfcn"
	case strings.Contains(mode, "WCDMA") || strings.Contains(mode, "UMTS"):
		radio.AccessTechnology = "umts"
		radio.ChannelType = "uarfcn"
	case strings.Contains(mode, "GSM"):
		radio.AccessTechnology = "gsm"
		radio.ChannelType = "arfcn"
	default:
		return nil, errServingRadioResponse
	}
	if strings.Contains(mode, "FDD") {
		radio.DuplexMode = "fdd"
	} else if strings.Contains(mode, "TDD") {
		radio.DuplexMode = "tdd"
	}
	radio.Band = servingBandLabel(radio.AccessTechnology, fields[2])

	channelValue := strings.TrimSpace(fields[3])
	if channelValue != "" && channelValue != "-" {
		channel, parseErr := strconv.ParseUint(channelValue, 10, 32)
		if parseErr != nil {
			return nil, errServingRadioResponse
		}
		value := uint32(channel)
		radio.Channel = &value
	}
	return radio, nil
}

func servingBandLabel(accessTechnology, value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" || normalized == "-" {
		return ""
	}
	parts := strings.Fields(normalized)
	last := parts[len(parts)-1]
	switch accessTechnology {
	case "lte":
		last = strings.TrimPrefix(last, "B")
		if positiveDecimal(last) {
			return "B" + last
		}
	case "nr5g":
		last = strings.TrimPrefix(strings.ToLower(last), "n")
		if positiveDecimal(last) {
			return "n" + last
		}
	}
	return normalized
}

func positiveDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	number, err := strconv.ParseUint(value, 10, 32)
	return err == nil && number > 0
}

func cloneServingRadio(value *domain.ServingRadio) *domain.ServingRadio {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Channel != nil {
		channel := *value.Channel
		cloned.Channel = &channel
	}
	return &cloned
}
