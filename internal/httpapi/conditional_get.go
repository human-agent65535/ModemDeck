package httpapi

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
)

// prepareConditionalCollectionRead validates an in-memory representation using
// the process-local durable watermark. The tag is deliberately coarse: an
// unrelated durable write may cause one extra collection read, but an unchanged
// watermark can bypass SQLite entirely without a per-resource invalidation tree.
func (api *API) prepareConditionalCollectionRead(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	if api.runtimeEvents == nil || request.Method != http.MethodGet {
		return "", false
	}
	signal := api.runtimeEvents.Current()
	principalID := "anonymous"
	if principal, ok := auth.PrincipalFromContext(request.Context()); ok {
		principalID = principal.UserID
	}
	identity := principalID + "\x00" + request.URL.RequestURI()
	digest := sha256.Sum256([]byte(identity))
	etag := fmt.Sprintf(
		`W/"%s-%d-%x"`,
		signal.Epoch,
		signal.DataRevision,
		digest[:8],
	)
	response.Header().Set("ETag", etag)
	response.Header().Set("Cache-Control", "no-store")
	if !etagListContains(request.Header.Get("If-None-Match"), etag) {
		return etag, false
	}
	response.WriteHeader(http.StatusNotModified)
	return etag, true
}

func etagListContains(value, target string) bool {
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == target {
			return true
		}
	}
	return false
}
