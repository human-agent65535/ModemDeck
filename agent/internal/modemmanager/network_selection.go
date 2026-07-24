package modemmanager

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	networkScanTimeout      = 120 * time.Second
	networkSelectionTimeout = 45 * time.Second
)

type accessTechnologyName struct {
	mask uint32
	name string
}

var accessTechnologyNames = []accessTechnologyName{
	{mask: 1 << 0, name: "pots"},
	{mask: 1 << 1, name: "gsm"},
	{mask: 1 << 2, name: "gsm-compact"},
	{mask: 1 << 3, name: "gprs"},
	{mask: 1 << 4, name: "edge"},
	{mask: 1 << 5, name: "umts"},
	{mask: 1 << 6, name: "hsdpa"},
	{mask: 1 << 7, name: "hsupa"},
	{mask: 1 << 8, name: "hspa"},
	{mask: 1 << 9, name: "hspa-plus"},
	{mask: 1 << 10, name: "1xrtt"},
	{mask: 1 << 11, name: "evdo0"},
	{mask: 1 << 12, name: "evdoa"},
	{mask: 1 << 13, name: "evdob"},
	{mask: 1 << 14, name: "lte"},
	{mask: 1 << 15, name: "5g-nr"},
	{mask: 1 << 16, name: "lte-cat-m"},
	{mask: 1 << 17, name: "lte-nb-iot"},
}

func (p *Provider) ScanNetworks(
	ctx context.Context,
	request domain.NetworkScanRequest,
) (domain.NetworkScanResult, error) {
	const operation = "scan_networks"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.NetworkScanResult{}, err
	}
	if request.LineID == "" {
		return domain.NetworkScanResult{}, domain.InvalidArgument(operation, "line id is required")
	}
	bounded, cancel, err := networkOperationContext(ctx, networkScanTimeout, operation)
	if err != nil {
		return domain.NetworkScanResult{}, err
	}
	defer cancel()

	modemPath, err := p.networkSelectionModemPath(bounded, request.LineID, operation)
	if err != nil {
		return domain.NetworkScanResult{}, err
	}
	if !p.claimNetworkOperation(request.LineID) {
		return domain.NetworkScanResult{}, domain.Conflict(
			operation,
			"another network selection operation is already running for this line",
		)
	}
	defer p.releaseNetworkOperation(request.LineID)

	body, err := p.call(
		bounded,
		modemPath,
		modem3GPPInterface+".Scan",
		operation,
		"ModemManager failed to scan available networks",
	)
	if err != nil {
		return domain.NetworkScanResult{}, err
	}
	networks, err := parseNetworkScan(body)
	if err != nil {
		return domain.NetworkScanResult{}, domain.Internal(
			operation,
			"ModemManager network scan response was malformed",
			err,
		)
	}
	return domain.NetworkScanResult{
		RequestID:  request.RequestID,
		LineID:     request.LineID,
		ObservedAt: p.now().UTC(),
		Networks:   networks,
	}, nil
}

func (p *Provider) SetNetworkSelection(
	ctx context.Context,
	request domain.ApplyNetworkSelectionRequest,
) (domain.NetworkSelectionReceipt, error) {
	const operation = "set_network_selection"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.Mode = domain.NetworkSelectionMode(strings.TrimSpace(string(request.Mode)))
	request.OperatorCode = strings.TrimSpace(request.OperatorCode)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.NetworkSelectionReceipt{}, err
	}
	if request.LineID == "" {
		return domain.NetworkSelectionReceipt{}, domain.InvalidArgument(operation, "line id is required")
	}
	switch request.Mode {
	case domain.NetworkSelectionModeAuto:
		if request.OperatorCode != "" {
			return domain.NetworkSelectionReceipt{}, domain.InvalidArgument(
				operation,
				"operator_code must be empty in auto mode",
			)
		}
	case domain.NetworkSelectionModeManual:
		if !validOperatorCode(request.OperatorCode) {
			return domain.NetworkSelectionReceipt{}, domain.InvalidArgument(
				operation,
				"operator_code must contain 5 or 6 digits in manual mode",
			)
		}
	default:
		return domain.NetworkSelectionReceipt{}, domain.InvalidArgument(
			operation,
			"mode must be auto or manual",
		)
	}

	bounded, cancel, err := networkOperationContext(
		ctx,
		networkSelectionTimeout,
		operation,
	)
	if err != nil {
		return domain.NetworkSelectionReceipt{}, err
	}
	defer cancel()
	modemPath, err := p.networkSelectionModemPath(bounded, request.LineID, operation)
	if err != nil {
		return domain.NetworkSelectionReceipt{}, err
	}
	if !p.claimNetworkOperation(request.LineID) {
		return domain.NetworkSelectionReceipt{}, domain.Conflict(
			operation,
			"another network selection operation is already running for this line",
		)
	}
	defer p.releaseNetworkOperation(request.LineID)

	operatorCode := request.OperatorCode
	if request.Mode == domain.NetworkSelectionModeAuto {
		operatorCode = ""
	}
	if _, err := p.call(
		bounded,
		modemPath,
		modem3GPPInterface+".Register",
		operation,
		"ModemManager failed to register the selected network",
		operatorCode,
	); err != nil {
		return domain.NetworkSelectionReceipt{}, err
	}
	return domain.NetworkSelectionReceipt{
		RequestID:    request.RequestID,
		LineID:       request.LineID,
		Mode:         request.Mode,
		OperatorCode: request.OperatorCode,
		AppliedAt:    p.now().UTC(),
	}, nil
}

func (p *Provider) networkSelectionModemPath(
	ctx context.Context,
	lineID string,
	operation string,
) (dbus.ObjectPath, error) {
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return "", err
	}
	parsed := ParseManagedObjects(objects, p.ids)
	line, found := findLine(parsed.Lines, lineID)
	if !found {
		return "", domain.NotFound(operation, "line was not found")
	}
	modemPath, found := parsed.LinePaths[line.ID]
	if !found {
		return "", domain.Internal(operation, "line path was missing", nil)
	}
	interfaces, found := objects[modemPath]
	if !found {
		return "", domain.Internal(operation, "line object was missing", nil)
	}
	if _, found := interfaces[modem3GPPInterface]; !found {
		return "", domain.NotSupported(
			operation,
			"line does not expose the ModemManager 3GPP interface",
		)
	}
	return modemPath, nil
}

func (p *Provider) claimNetworkOperation(lineID string) bool {
	p.networkOperationMu.Lock()
	defer p.networkOperationMu.Unlock()
	if p.networkOperations == nil {
		p.networkOperations = make(map[string]struct{})
	}
	if _, found := p.networkOperations[lineID]; found {
		return false
	}
	p.networkOperations[lineID] = struct{}{}
	return true
}

func (p *Provider) releaseNetworkOperation(lineID string) {
	p.networkOperationMu.Lock()
	defer p.networkOperationMu.Unlock()
	delete(p.networkOperations, lineID)
}

func networkOperationContext(
	ctx context.Context,
	timeout time.Duration,
	operation string,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, domain.InvalidArgument(operation, "request context is required")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	return bounded, cancel, nil
}

func parseNetworkScan(body []any) ([]domain.MobileNetwork, error) {
	var raw []map[string]dbus.Variant
	if err := dbus.Store(body, &raw); err != nil {
		return nil, err
	}
	candidates := make([]domain.MobileNetwork, 0, len(raw))
	for index, properties := range raw {
		statusCode, found := uint32Property(properties, "status")
		if !found {
			return nil, fmt.Errorf("network %d has an invalid status", index)
		}
		operatorCode, found := stringProperty(properties, "operator-code")
		operatorCode = strings.TrimSpace(operatorCode)
		if !found || !validOperatorCode(operatorCode) {
			return nil, fmt.Errorf("network %d has an invalid operator-code", index)
		}
		operatorLong, err := optionalScanString(properties, "operator-long")
		if err != nil {
			return nil, fmt.Errorf("network %d: %w", index, err)
		}
		operatorShort, err := optionalScanString(properties, "operator-short")
		if err != nil {
			return nil, fmt.Errorf("network %d: %w", index, err)
		}
		accessTechnologies, err := optionalScanUint32(properties, "access-technology")
		if err != nil {
			return nil, fmt.Errorf("network %d: %w", index, err)
		}
		candidates = append(candidates, domain.MobileNetwork{
			Status:             networkAvailability(statusCode),
			OperatorCode:       operatorCode,
			OperatorLong:       strings.TrimSpace(operatorLong),
			OperatorShort:      strings.TrimSpace(operatorShort),
			AccessTechnologies: accessTechnologies,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.OperatorCode != right.OperatorCode {
			return left.OperatorCode < right.OperatorCode
		}
		if networkAvailabilityRank(left.Status) != networkAvailabilityRank(right.Status) {
			return networkAvailabilityRank(left.Status) < networkAvailabilityRank(right.Status)
		}
		if left.OperatorLong != right.OperatorLong {
			return left.OperatorLong < right.OperatorLong
		}
		if left.OperatorShort != right.OperatorShort {
			return left.OperatorShort < right.OperatorShort
		}
		return left.AccessTechnologies < right.AccessTechnologies
	})

	networks := make([]domain.MobileNetwork, 0, len(candidates))
	for _, candidate := range candidates {
		if len(networks) == 0 ||
			networks[len(networks)-1].OperatorCode != candidate.OperatorCode {
			networks = append(networks, candidate)
			continue
		}
		current := &networks[len(networks)-1]
		current.AccessTechnologies |= candidate.AccessTechnologies
		if current.OperatorLong == "" {
			current.OperatorLong = candidate.OperatorLong
		}
		if current.OperatorShort == "" {
			current.OperatorShort = candidate.OperatorShort
		}
	}
	for index := range networks {
		networks[index].AccessTechnologyNames = accessTechnologyLabels(
			networks[index].AccessTechnologies,
		)
	}
	sort.Slice(networks, func(i, j int) bool {
		left, right := networks[i], networks[j]
		if networkAvailabilityRank(left.Status) != networkAvailabilityRank(right.Status) {
			return networkAvailabilityRank(left.Status) < networkAvailabilityRank(right.Status)
		}
		return left.OperatorCode < right.OperatorCode
	})
	return networks, nil
}

func optionalScanString(properties Properties, name string) (string, error) {
	variant, found := properties[name]
	if !found {
		return "", nil
	}
	value, ok := variant.Value().(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", name)
	}
	return value, nil
}

func optionalScanUint32(properties Properties, name string) (uint32, error) {
	variant, found := properties[name]
	if !found {
		return 0, nil
	}
	value, ok := variant.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("%s is not an unsigned integer", name)
	}
	return value, nil
}

func validOperatorCode(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func networkAvailability(value uint32) domain.NetworkAvailability {
	switch value {
	case 1:
		return domain.NetworkAvailabilityAvailable
	case 2:
		return domain.NetworkAvailabilityCurrent
	case 3:
		return domain.NetworkAvailabilityForbidden
	default:
		return domain.NetworkAvailabilityUnknown
	}
}

func networkAvailabilityRank(value domain.NetworkAvailability) int {
	switch value {
	case domain.NetworkAvailabilityCurrent:
		return 0
	case domain.NetworkAvailabilityAvailable:
		return 1
	case domain.NetworkAvailabilityForbidden:
		return 2
	default:
		return 3
	}
}

func accessTechnologyLabels(value uint32) []string {
	names := make([]string, 0)
	for _, technology := range accessTechnologyNames {
		if value&technology.mask != 0 {
			names = append(names, technology.name)
		}
	}
	return names
}

var _ domain.NetworkSelectionProvider = (*Provider)(nil)
