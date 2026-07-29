package modemmanager

type smsPDUKind string

const (
	smsPDUUnknown                    uint32 = 0
	smsPDUDeliver                    uint32 = 1
	smsPDUSubmit                     uint32 = 2
	smsPDUStatusReport               uint32 = 3
	smsPDUCDMADeliver                uint32 = 32
	smsPDUCDMASubmit                 uint32 = 33
	smsPDUCDMACancellation           uint32 = 34
	smsPDUCDMADeliveryAcknowledgment uint32 = 35
	smsPDUCDMAUserAcknowledgment     uint32 = 36
	smsPDUCDMAReadAcknowledgment     uint32 = 37

	smsKindUnknown         smsPDUKind = "unknown"
	smsKindDeliver         smsPDUKind = "deliver"
	smsKindSubmit          smsPDUKind = "submit"
	smsKindStatusReport    smsPDUKind = "status_report"
	smsKindProtocolControl smsPDUKind = "protocol_control"
)

func classifySMSPDU(code uint32) smsPDUKind {
	switch code {
	case smsPDUDeliver, smsPDUCDMADeliver:
		return smsKindDeliver
	case smsPDUSubmit, smsPDUCDMASubmit:
		return smsKindSubmit
	case smsPDUStatusReport:
		return smsKindStatusReport
	case smsPDUCDMACancellation,
		smsPDUCDMADeliveryAcknowledgment,
		smsPDUCDMAUserAcknowledgment,
		smsPDUCDMAReadAcknowledgment:
		return smsKindProtocolControl
	case smsPDUUnknown:
		return smsKindUnknown
	default:
		return smsKindUnknown
	}
}

func smsBusinessDirection(kind smsPDUKind) (string, bool) {
	switch kind {
	case smsKindDeliver:
		return "incoming", true
	case smsKindSubmit:
		return "outgoing", true
	default:
		return "control", false
	}
}
