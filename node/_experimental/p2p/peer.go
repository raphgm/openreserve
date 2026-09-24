package p2p

import (
	"encoding/json"
	"fmt"
	"net"
)

// Peer represents a remote node connected over TCP
type Peer struct {
	conn       net.Conn
	ListenAddr string
	outbound   bool
}

// NewPeer constructs a Peer wrapper around a net.Conn
func NewPeer(conn net.Conn, outbound bool) *Peer {
	return &Peer{
		conn:     conn,
		outbound: outbound,
	}
}

// ReadLoop continuously reads incoming messages from the connection
func (p *Peer) ReadLoop(msgCh chan<- Message) {
	defer p.conn.Close()
	decoder := json.NewDecoder(p.conn)

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			fmt.Printf("peer read error: %v\\n", err)
			return // disconnect on error
		}

		// Send the decoded message to the server's processing channel
		msgCh <- msg
	}
}

// SendMessage JSON encodes and writes a message to the peer's TCP socket
func (p *Peer) SendMessage(msg Message) error {
	encoder := json.NewEncoder(p.conn)
	return encoder.Encode(msg)
}

// Close gracefully terminates the connection
func (p *Peer) Close() error {
	return p.conn.Close()
}
