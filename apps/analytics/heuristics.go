package main

import (
	"fmt"
	"sync"
	"time"
)

// ThreatLevel categorizes the severity of an anomaly
type ThreatLevel string

const (
	ThreatLow      ThreatLevel = "LOW"
	ThreatMedium   ThreatLevel = "MEDIUM"
	ThreatHigh     ThreatLevel = "HIGH"
	ThreatCritical ThreatLevel = "CRITICAL"
)

// Alert represents a flagged anomaly
type Alert struct {
	ID          string
	Address     string
	Type        string // e.g., "WHALE_MOVEMENT", "SYBIL_ATTACK"
	Description string
	Level       ThreatLevel
	Timestamp   time.Time
}

// HeuristicsEngine tracks state to identify malicious patterns
type HeuristicsEngine struct {
	mu           sync.RWMutex
	Alerts       []*Alert
	
	// Temporary tracking maps for stateful analysis
	TxCounts     map[string]int
	Volumes      map[string]float64
	UniqueTargets map[string]map[string]bool // Sender -> Set of Receivers
}

func NewHeuristicsEngine() *HeuristicsEngine {
	return &HeuristicsEngine{
		Alerts:        make([]*Alert, 0),
		TxCounts:      make(map[string]int),
		Volumes:       make(map[string]float64),
		UniqueTargets: make(map[string]map[string]bool),
	}
}

// Analyze evaluates a single transaction against rule-based models
func (he *HeuristicsEngine) Analyze(txHash, sender, receiver string, amount float64) {
	he.mu.Lock()
	defer he.mu.Unlock()

	he.TxCounts[sender]++
	he.Volumes[sender] += amount
	
	if he.UniqueTargets[sender] == nil {
		he.UniqueTargets[sender] = make(map[string]bool)
	}
	he.UniqueTargets[sender][receiver] = true

	// 1. Whale Detection (> 50,000 ORP in one tx)
	if amount > 50000.0 {
		he.flag(sender, "WHALE_MOVEMENT", fmt.Sprintf("Massive transfer of %.2f ORP", amount), ThreatMedium)
	}

	// 2. Wash Trading (High Tx Count, High Volume)
	if he.TxCounts[sender] > 100 && he.Volumes[sender] > 100000.0 {
		he.flag(sender, "WASH_TRADING", "High frequency trading detected from a single node", ThreatHigh)
	}

	// 3. Sybil Attack (1 sender sending to >50 unique addresses rapidly)
	if len(he.UniqueTargets[sender]) > 50 {
		he.flag(sender, "SYBIL_ATTACK", "Sending funds to 50+ unique addresses. Possible airdrop farming or sybil.", ThreatCritical)
	}
}

func (he *HeuristicsEngine) flag(address, alertType, desc string, level ThreatLevel) {
	alert := &Alert{
		ID:          fmt.Sprintf("ALT-%d", time.Now().UnixNano()),
		Address:     address,
		Type:        alertType,
		Description: desc,
		Level:       level,
		Timestamp:   time.Now(),
	}
	he.Alerts = append(he.Alerts, alert)
	fmt.Printf("[ALERT] %s | %s | %s\\n", level, alertType, address)
}
