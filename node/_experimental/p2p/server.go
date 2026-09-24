package p2p

import (
	"fmt"
	"net"
	"sync"
)

// Server manages the P2P networking, incoming connections, and active peers
type Server struct {
	ListenAddr string
	listener   net.Listener

	mu    sync.RWMutex
	peers map[*Peer]bool

	msgCh chan Message
}

// NewServer creates a new P2P Server instance
func NewServer(listenAddr string) *Server {
	return &Server{
		ListenAddr: listenAddr,
		peers:      make(map[*Peer]bool),
		msgCh:      make(chan Message, 1024),
	}
}

// Start opens the TCP listener and begins accepting incoming peer connections
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.ListenAddr)
	if err != nil {
		return err
	}
	s.listener = ln
	fmt.Printf("ORP Node listening on %s\\n", s.ListenAddr)

	go s.acceptLoop()
	go s.processMessages()

	return nil
}

// acceptLoop handles incoming TCP connections
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			fmt.Printf("accept error: %v\\n", err)
			continue
		}

		peer := NewPeer(conn, false)
		s.addPeer(peer)

		// Start reading messages from this peer
		go peer.ReadLoop(s.msgCh)
	}
}

// processMessages routes incoming messages to the blockchain core
func (s *Server) processMessages() {
	for msg := range s.msgCh {
		// In Phase 6, we will route these to the State Machine / Consensus Engine
		fmt.Printf("Received message type: %d, size: %d bytes\\n", msg.Type, len(msg.Payload))
	}
}

// Broadcast sends a message to all connected peers
func (s *Server) Broadcast(msg Message) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for peer := range s.peers {
		go func(p *Peer) {
			if err := p.SendMessage(msg); err != nil {
				fmt.Printf("failed to send to peer: %v\\n", err)
			}
		}(peer)
	}
}

// Connect dials an outbound connection to another known node
func (s *Server) Connect(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	peer := NewPeer(conn, true)
	s.addPeer(peer)
	go peer.ReadLoop(s.msgCh)

	return nil
}

// addPeer safely adds a peer to the server's connected map
func (s *Server) addPeer(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p] = true
	fmt.Printf("New peer connected: %s\\n", p.conn.RemoteAddr().String())
}
