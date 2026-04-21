package mcp

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
