package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type DashboardAPI struct {
	Engine *HeuristicsEngine
}

func NewDashboardAPI(engine *HeuristicsEngine) *DashboardAPI {
	return &DashboardAPI{
		Engine: engine,
	}
}

func (api *DashboardAPI) Start() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/alerts", api.handleGetAlerts)

	fmt.Println("Analytics Dashboard API listening on :6000")
	log.Fatal(http.ListenAndServe(":6000", mux))
}

func (api *DashboardAPI) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	api.Engine.mu.RLock()
	alerts := api.Engine.Alerts
	api.Engine.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_alerts": len(alerts),
		"alerts":       alerts,
	})
}

func main() {
	fmt.Println("Initializing OpenReserve Analytics & ML Engine...")
	
	engine := NewHeuristicsEngine()
	
	monitor := NewMonitor(engine)
	monitor.Start()

	dashboard := NewDashboardAPI(engine)
	dashboard.Start()
}
