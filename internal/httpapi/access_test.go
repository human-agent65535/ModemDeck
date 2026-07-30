package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestLineInventoryAndDevicesFollowExplicitAssignmentsForAdminAndMember(t *testing.T) {
	t.Parallel()

	lines := []store.LineSummary{
		{ID: "line-allowed"},
		{ID: "line-hidden"},
	}
	devices := []store.Device{
		{IMEI: "allowed", SIM: &store.SIMCard{LineID: "line-allowed"}},
		{IMEI: "hidden", SIM: &store.SIMCard{LineID: "line-hidden"}},
		{IMEI: "inventory-only"},
	}
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleMember} {
		ctx := auth.ContextWithPrincipal(context.Background(), auth.Principal{
			UserID:         "user-test",
			Role:           role,
			AllowedLineIDs: []string{"line-allowed"},
		})
		visibleLines := filterLinesForPrincipal(ctx, lines)
		if len(visibleLines) != 1 || visibleLines[0].ID != "line-allowed" {
			t.Fatalf("%s visible lines = %+v", role, visibleLines)
		}
		visibleDevices := filterDevicesForPrincipal(ctx, devices)
		if len(visibleDevices) != 1 || visibleDevices[0].IMEI != "allowed" {
			t.Fatalf("%s visible devices = %+v", role, visibleDevices)
		}
	}
}

func TestAdminOnlyRoutesExcludeLineOwnedConfiguration(t *testing.T) {
	t.Parallel()

	for _, route := range []struct {
		path   string
		method string
	}{
		{"/api/v1/network", http.MethodGet},
		{"/api/v1/proxies", http.MethodPost},
		{"/api/v1/proxies/proxy-1", http.MethodPatch},
		{"/api/v1/settings/lines", http.MethodPatch},
		{"/api/v1/settings/system", http.MethodPatch},
		{"/api/v1/settings/recording", http.MethodPut},
		{"/api/v1/settings/telegram", http.MethodPost},
		{"/api/v1/settings/telegram/bot-1", http.MethodPut},
		{"/api/v1/lines/line-1/label", http.MethodPatch},
		{"/api/v1/devices/line-1/configuration", http.MethodPatch},
		{"/api/v1/devices/line-1/network-selection", http.MethodPut},
		{"/api/v1/devices/line-1/profiles", http.MethodPut},
	} {
		if adminOnlyAPIPath(route.path, route.method) {
			t.Fatalf("%s %s is incorrectly admin-only", route.method, route.path)
		}
	}
	for _, route := range []struct {
		path   string
		method string
	}{
		{"/api/v1/devices", http.MethodPost},
		{"/api/v1/devices/867530900000001", http.MethodPatch},
		{"/api/v1/diagnostics", http.MethodGet},
	} {
		if !adminOnlyAPIPath(route.path, route.method) {
			t.Fatalf("%s %s must remain admin-only", route.method, route.path)
		}
	}
}
