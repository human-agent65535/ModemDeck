package store

import (
	"strings"
)

func MessageThreadKey(lineID, peer string) string {
	return strings.TrimSpace(lineID) + "|" + strings.TrimSpace(peer)
}
