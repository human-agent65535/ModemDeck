package modemmanager

import (
	"sort"
	"sync"

	"github.com/godbus/dbus/v5"
)

const defaultMessagePropertyCacheLimit = 2048

type messagePropertyCacheKey struct {
	modemPath   dbus.ObjectPath
	messagePath dbus.ObjectPath
}

type cachedMessageProperties struct {
	properties Properties
	lastUsed   uint64
}

type messagePropertyCache struct {
	mu       sync.Mutex
	epoch    string
	entries  map[messagePropertyCacheKey]cachedMessageProperties
	sequence uint64
	limit    int
}

func newMessagePropertyCache(limit int) *messagePropertyCache {
	if limit < 1 {
		limit = 1
	}
	return &messagePropertyCache{
		entries: make(map[messagePropertyCacheKey]cachedMessageProperties),
		limit:   limit,
	}
}

func (c *messagePropertyCache) synchronize(
	epoch string,
	fn func(*messagePropertyCache) error,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != epoch {
		c.resetLocked(epoch)
	}
	return fn(c)
}

func (c *messagePropertyCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.resetLocked("")
	c.mu.Unlock()
}

func (c *messagePropertyCache) resetLocked(epoch string) {
	c.epoch = epoch
	c.entries = make(map[messagePropertyCacheKey]cachedMessageProperties)
	c.sequence = 0
}

func (c *messagePropertyCache) getLocked(
	key messagePropertyCacheKey,
) (Properties, bool) {
	entry, found := c.entries[key]
	if !found {
		return nil, false
	}
	c.sequence++
	entry.lastUsed = c.sequence
	c.entries[key] = entry
	return cloneMessageProperties(entry.properties), true
}

func (c *messagePropertyCache) storeLocked(
	key messagePropertyCacheKey,
	properties Properties,
) {
	if !completeTerminalMessageProperties(properties) {
		delete(c.entries, key)
		return
	}
	c.sequence++
	c.entries[key] = cachedMessageProperties{
		properties: cloneMessageProperties(properties),
		lastUsed:   c.sequence,
	}
}

func (c *messagePropertyCache) deleteLocked(key messagePropertyCacheKey) {
	delete(c.entries, key)
}

func (c *messagePropertyCache) reconcileLocked(
	listed map[messagePropertyCacheKey]struct{},
) {
	for key := range c.entries {
		if _, found := listed[key]; !found {
			delete(c.entries, key)
		}
	}
	c.enforceLimitLocked()
}

func (c *messagePropertyCache) enforceLimitLocked() {
	overflow := len(c.entries) - c.limit
	if overflow <= 0 {
		return
	}
	type candidate struct {
		key      messagePropertyCacheKey
		lastUsed uint64
	}
	candidates := make([]candidate, 0, len(c.entries))
	for key, entry := range c.entries {
		candidates = append(candidates, candidate{
			key:      key,
			lastUsed: entry.lastUsed,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].lastUsed != candidates[j].lastUsed {
			return candidates[i].lastUsed < candidates[j].lastUsed
		}
		if candidates[i].key.modemPath != candidates[j].key.modemPath {
			return candidates[i].key.modemPath < candidates[j].key.modemPath
		}
		return candidates[i].key.messagePath < candidates[j].key.messagePath
	})
	for _, candidate := range candidates[:overflow] {
		delete(c.entries, candidate.key)
	}
}

func (c *messagePropertyCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func cloneMessageProperties(properties Properties) Properties {
	cloned := make(Properties, len(properties))
	for name, value := range properties {
		cloned[name] = value
	}
	return cloned
}

func completeTerminalMessageProperties(properties Properties) bool {
	state, stateKnown := uint32Property(properties, "State")
	if !stateKnown || state != 3 && state != 5 {
		return false
	}
	pduType, pduTypeKnown := uint32Property(properties, "PduType")
	if !pduTypeKnown {
		return false
	}
	if classifySMSPDU(pduType) == smsKindStatusReport {
		if _, found := stringProperty(properties, "Number"); !found {
			return false
		}
		if _, found := uint32Property(properties, "MessageReference"); !found {
			return false
		}
		if _, found := uint32Property(properties, "DeliveryState"); !found {
			return false
		}
		if _, found := stringProperty(properties, "DischargeTimestamp"); found {
			return true
		}
		_, found := stringProperty(properties, "Timestamp")
		return found
	}
	if messageDirectionName(pduType) == "unknown" {
		return false
	}
	if _, found := stringProperty(properties, "Number"); !found {
		return false
	}
	if _, found := stringProperty(properties, "Timestamp"); !found {
		return false
	}
	if _, found := stringProperty(properties, "Text"); found {
		return true
	}
	data, found := propertyValue(properties, "Data")
	if !found {
		return false
	}
	_, valid := data.([]byte)
	return valid
}
