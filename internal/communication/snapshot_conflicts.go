package communication

import (
	"sort"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/phone"
)

func quarantineDuplicateSubscriptionAttachments(
	snapshot agentclient.Snapshot,
) (agentclient.Snapshot, []string) {
	snapshot.Lines = append([]agentclient.Line(nil), snapshot.Lines...)
	snapshot.Calls = append([]agentclient.Call(nil), snapshot.Calls...)
	snapshot.Messages = append([]agentclient.Message(nil), snapshot.Messages...)
	if len(snapshot.Lines) < 2 {
		return snapshot, nil
	}

	parents := make([]int, len(snapshot.Lines))
	for index := range parents {
		parents[index] = index
	}
	var find func(int) int
	find = func(index int) int {
		if parents[index] != index {
			parents[index] = find(parents[index])
		}
		return parents[index]
	}
	union := func(left, right int) {
		leftRoot := find(left)
		rightRoot := find(right)
		if leftRoot != rightRoot {
			parents[rightRoot] = leftRoot
		}
	}

	owners := make(map[string]int)
	for index, line := range snapshot.Lines {
		for _, identity := range subscriptionAttachmentIdentities(line) {
			if owner, found := owners[identity]; found {
				union(index, owner)
			} else {
				owners[identity] = index
			}
		}
	}

	groups := make(map[int][]int)
	for index, line := range snapshot.Lines {
		if len(subscriptionAttachmentIdentities(line)) == 0 {
			continue
		}
		root := find(index)
		groups[root] = append(groups[root], index)
	}

	quarantined := make(map[string]struct{})
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		registered := make([]int, 0, len(group))
		for _, index := range group {
			if subscriptionAttachmentRegistered(snapshot.Lines[index]) {
				registered = append(registered, index)
			}
		}
		winner := -1
		if len(registered) == 1 {
			winner = registered[0]
		} else if len(registered) == 0 {
			present := make([]int, 0, len(group))
			for _, index := range group {
				if snapshot.Lines[index].SIMPresent {
					present = append(present, index)
				}
			}
			if len(present) == 1 {
				winner = present[0]
			}
		}
		for _, index := range group {
			if index == winner {
				continue
			}
			lineID := strings.TrimSpace(snapshot.Lines[index].ID)
			if lineID == "" {
				continue
			}
			quarantined[lineID] = struct{}{}
			snapshot.Lines[index] = quarantineSubscription(snapshot.Lines[index])
		}
	}
	if len(quarantined) == 0 {
		return snapshot, nil
	}

	calls := snapshot.Calls[:0]
	for _, call := range snapshot.Calls {
		if _, blocked := quarantined[strings.TrimSpace(call.LineID)]; !blocked {
			calls = append(calls, call)
		}
	}
	snapshot.Calls = calls
	messages := snapshot.Messages[:0]
	for _, message := range snapshot.Messages {
		if _, blocked := quarantined[strings.TrimSpace(message.LineID)]; !blocked {
			messages = append(messages, message)
		}
	}
	snapshot.Messages = messages

	lineIDs := make([]string, 0, len(quarantined))
	for lineID := range quarantined {
		lineIDs = append(lineIDs, lineID)
	}
	sort.Strings(lineIDs)
	return snapshot, lineIDs
}

func subscriptionAttachmentIdentities(line agentclient.Line) []string {
	identities := make([]string, 0, 3)
	if iccid := strings.ToLower(strings.TrimSpace(line.SIMIdentifier)); iccid != "" {
		identities = append(identities, "iccid:"+iccid)
	}
	if imsi := strings.ToLower(strings.TrimSpace(line.IMSI)); imsi != "" {
		identities = append(identities, "imsi:"+imsi)
	}
	if len(line.OwnNumbers) > 0 {
		if number := phone.CanonicalNetworkAddress(
			strings.TrimSpace(line.OwnNumbers[0]),
			line.HomeCountryISO,
		); number != "" {
			identities = append(identities, "phone:"+number)
		}
	}
	return identities
}

func subscriptionAttachmentRegistered(line agentclient.Line) bool {
	if line.EmergencyOnly {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line.State)) {
	case "connected", "registered":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(line.RegistrationState)) {
	case "home", "registered", "roaming":
		return true
	default:
		return false
	}
}

func quarantineSubscription(line agentclient.Line) agentclient.Line {
	line.OwnNumbers = nil
	line.SIMPresent = false
	line.SIMPath = ""
	line.SIMIdentifier = ""
	line.IMSI = ""
	line.HomeOperatorCode = ""
	line.HomeOperatorName = ""
	line.HomeCountryISO = ""
	line.ServingOperatorCode = ""
	line.ServingOperatorName = ""
	line.ServingCountryISO = ""
	line.RegistrationStateKnown = false
	line.RegistrationStateCode = 0
	line.RegistrationState = ""
	line.Roaming = false
	line.OperatorIdentifier = ""
	line.OperatorName = ""
	line.EmergencyNumbers = nil
	line.EmergencyOnly = false
	line.CallIDs = nil
	line.MessageIDs = nil
	line.SupportedMessageStorages = nil
	line.DefaultMessageStorage = 0
	line.Capabilities.SIMInterface = false
	line.Capabilities.VoiceInterface = false
	line.Capabilities.MessagingInterface = false
	line.Capabilities.Dial = false
	line.Capabilities.AnswerCall = false
	line.Capabilities.HangupCall = false
	line.Capabilities.RejectCall = false
	line.Capabilities.SendDTMF = false
	line.Capabilities.SendMessage = false
	line.Capabilities.Media = false
	return line
}
