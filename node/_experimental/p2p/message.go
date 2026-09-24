package p2p

// MessageType defines the kind of payload being transmitted over the wire
type MessageType int

const (
	MsgHandshake MessageType = iota
	MsgTxBroadcast
	MsgBlockBroadcast
)

// Message is the standard envelope for all P2P network traffic
type Message struct {
	Type    MessageType `json:"type"`
	Payload []byte      `json:"payload"`
}

// HandshakePayload is sent when two nodes first connect
type HandshakePayload struct {
	Version    uint32 `json:"version"`
	ListenAddr string `json:"listenAddr"`
}
