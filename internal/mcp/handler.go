package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/mgt-tool/mgtt/internal/incident"
	"github.com/mgt-tool/mgtt/internal/model"
)

// Handler holds the business logic for all MCP tool methods. Each method
// maps 1:1 to an MCP tool registered in server.go. No SDK types leak here.
type Handler struct {
	cfg Config
}

// NewHandler creates a Handler backed by cfg.
func NewHandler(cfg Config) *Handler {
	return &Handler{cfg: cfg}
}

// AboutResult is the payload returned by the about tool.
type AboutResult struct {
	Version       string   `json:"version"`
	Transports    []string `json:"transports"`
	ReadonlyOnly  bool     `json:"readonly_only"`
	OnWrite       string   `json:"on_write"`
	MaxExecute    int      `json:"max_execute_per_incident"`
	ProbeTimeoutS int      `json:"probe_timeout_seconds"`
}

// About returns server metadata — version, active transports, current safety
// posture. No state, no engine. Used by agents for capability discovery.
func (h *Handler) About() (*AboutResult, error) {
	transports := []string{"stdio"}
	if h.cfg.HTTP {
		transports = []string{"http"}
	}
	v := h.cfg.Version
	if v == "" {
		v = "dev"
	}
	onWrite := h.cfg.OnWrite
	if onWrite == "" {
		onWrite = "run"
	}
	return &AboutResult{
		Version:       v,
		Transports:    transports,
		ReadonlyOnly:  h.cfg.ReadonlyOnly,
		OnWrite:       onWrite,
		MaxExecute:    h.cfg.MaxExecutePerIncident,
		ProbeTimeoutS: h.cfg.ProbeTimeoutSeconds,
	}, nil
}

// IncidentStartParams is the input for incident.start. ModelRef is a path
// to a system.model.yaml on disk the server can read. ID is optional —
// when empty the server generates one. Suspect is accepted for forward
// compatibility but not consumed in Phase 1.
type IncidentStartParams struct {
	ModelRef string   `json:"model_ref"`
	ID       string   `json:"id,omitempty"`
	Suspect  []string `json:"suspect,omitempty"`
}

// IncidentStartResult carries the persistent incident identifier an agent
// passes to every subsequent tool call (fact.add, plan, probe, snapshot).
type IncidentStartResult struct {
	IncidentID string `json:"incident_id"`
}

// IncidentStart creates a new incident bound to the model at p.ModelRef.
// Unlike the CLI path, it does not write `.mgtt-current`: multiple incidents
// may run concurrently against the same $MGTT_HOME (design D5).
func (h *Handler) IncidentStart(p IncidentStartParams) (*IncidentStartResult, error) {
	if p.ModelRef == "" {
		return nil, fmt.Errorf("model_ref is required")
	}
	absRef, err := filepath.Abs(p.ModelRef)
	if err != nil {
		return nil, fmt.Errorf("resolve model path: %w", err)
	}
	m, err := model.Load(absRef)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	inc, err := incident.StartIsolated(m.Meta.Name, m.Meta.Version, p.ID, absRef)
	if err != nil {
		return nil, fmt.Errorf("start incident: %w", err)
	}
	return &IncidentStartResult{IncidentID: inc.ID}, nil
}

// IncidentEndParams is the input for incident.end. Verdict is optional.
type IncidentEndParams struct {
	IncidentID string `json:"incident_id"`
	Verdict    string `json:"verdict,omitempty"`
}

// IncidentEndResult confirms the incident has been marked ended on disk.
type IncidentEndResult struct {
	Saved bool `json:"saved"`
}

// IncidentEnd closes an incident — writes the end timestamp + optional
// verdict to the state file and returns confirmation. Does not touch
// `.mgtt-current`.
func (h *Handler) IncidentEnd(p IncidentEndParams) (*IncidentEndResult, error) {
	if p.IncidentID == "" {
		return nil, fmt.Errorf("incident_id is required")
	}
	mu := lockFor(p.IncidentID)
	mu.Lock()
	defer mu.Unlock()
	if _, err := incident.EndByID(p.IncidentID, p.Verdict); err != nil {
		return nil, fmt.Errorf("end incident: %w", err)
	}
	return &IncidentEndResult{Saved: true}, nil
}
