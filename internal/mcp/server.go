// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mcp exposes mgtt's constraint engine as an MCP service. See
// docs/superpowers/specs/2026-04-22-llm-support-design.md for the full
// contract. The handler reuses the CLI's engine path — engine.Plan,
// scenarios, facts, incident — via thin tool wrappers.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Config holds all runtime knobs for the MCP server.
type Config struct {
	Version               string // populated by the CLI from cli.version at startup
	HTTP                  bool
	Listen                string
	TokenEnv              string
	ReadonlyOnly          bool
	OnWrite               string // "pause" | "run" | "fail"
	MaxExecutePerIncident int
	ProbeTimeoutSeconds   int
	// Toolset picks the tools served: "diagnose" (incidents, probes),
	// "authoring" (types, model validation -- no live system, no writes)
	// or "all". "" means all.
	Toolset string
	// LegacyToolNames also registers the pre-0.4 dotted tool names
	// (incident.start, ...) as deprecated aliases. Off by default: some
	// clients and model APIs reject a tool list holding any name outside
	// [A-Za-z0-9_-], so the aliases are opt-in for one release.
	LegacyToolNames bool
}

// Run boots the MCP server with the given config. Blocks until the
// transport closes (stdin EOF for stdio, SIGINT/SIGTERM for HTTP).
func Run(cfg Config) error {
	if err := checkToolset(cfg.Toolset); err != nil {
		return err
	}
	s := buildServer(cfg)
	if cfg.HTTP {
		return runHTTP(s, cfg)
	}
	return server.ServeStdio(s)
}

// buildServer wires the handler and registers every tool. Exposed as a
// package-internal helper so the in-process client tests can drive the
// exact same server Run would expose on a transport.
func buildServer(cfg Config) *server.MCPServer {
	h := NewHandler(cfg)
	s := server.NewMCPServer("mgtt", cfg.Version, server.WithInstructions(serverInstructions))

	registerAbout(s, h)
	if serves(cfg.Toolset, "diagnose") {
		registerIncidentStart(s, h)
		registerIncidentEnd(s, h)
		registerFactAdd(s, h)
		registerFactsList(s, h)
		registerPlan(s, h)
		registerProbe(s, h)
		registerScenariosList(s, h)
		registerScenariosAlive(s, h)
		registerIncidentSnapshot(s, h)
		registerModelDiscover(s, h)
		if cfg.LegacyToolNames {
			registerLegacyAliases(s)
		}
	}
	if serves(cfg.Toolset, "authoring") {
		registerTypesList(s, h)
		registerTypesDescribe(s, h)
		registerModelValidate(s, h)
		registerScenarioSimulate(s, h)
		registerGuide(s, h)
		registerModelImpact(s, h)
		registerModelDiff(s, h)
	}

	return s
}

// toolsets are the values --toolset takes.
var toolsets = []string{"all", "diagnose", "authoring"}

func checkToolset(set string) error {
	if set == "" {
		return nil
	}
	for _, t := range toolsets {
		if set == t {
			return nil
		}
	}
	return fmt.Errorf("--toolset %q: want one of %v", set, toolsets)
}

// serves reports whether toolset set includes the tools of kind.
func serves(set, kind string) bool {
	return set == "" || set == "all" || set == kind
}

func registerTypesList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("types_list",
		mcpgo.WithDescription("List the component types the installed providers define, with how many facts each exposes. Use the names here in a model's type: fields; types_describe gives the detail."),
		mcpgo.WithString("provider", mcpgo.Description("optional: only this provider's types")),
		rawOutput(TypesListOutputSchema),
	)
	s.AddTool(tool, dispatch("types_list",
		func(req mcpgo.CallToolRequest) TypesListParams {
			return TypesListParams{Provider: req.GetString("provider", "")}
		},
		h.TypesList,
	))
}

func registerTypesDescribe(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("types_describe",
		mcpgo.WithDescription("Describe one type: its facts (use these names, never invented ones), its default healthy rules, its states with when-conditions and can_cause labels, and the variables its provider declares. A component's healthy: replaces these rules unless written as healthy: {add: [...]}; read them before overriding."),
		mcpgo.WithString("type", mcpgo.Required(), mcpgo.Description("type name, e.g. deployment or rds_instance")),
		mcpgo.WithString("provider", mcpgo.Description("optional: the provider, when several define the type")),
		rawOutput(TypesDescribeOutputSchema),
	)
	s.AddTool(tool, dispatch("types_describe",
		func(req mcpgo.CallToolRequest) TypesDescribeParams {
			return TypesDescribeParams{Type: req.GetString("type", ""), Provider: req.GetString("provider", "")}
		},
		h.TypesDescribe,
	))
}

func registerModelValidate(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("model_validate",
		mcpgo.WithDescription("Check a model against the installed types and report every error and warning at once: unknown dependencies, cycles, invalid need:, healthy: overrides that drop a type rule, rules reading a variable nothing sets, types no provider defines. Pass the model inline as model_source, or as a model_path the server can read. Reads nothing live, writes nothing."),
		mcpgo.WithString("model_path", mcpgo.Description("path to system.model.yaml on the server")),
		mcpgo.WithString("model_source", mcpgo.Description("the model YAML itself (max 512 KiB); use this when you cannot place a file where the server reads it")),
		rawOutput(ModelValidateOutputSchema),
	)
	s.AddTool(tool, dispatch("model_validate",
		func(req mcpgo.CallToolRequest) ModelValidateParams {
			return ModelValidateParams{ModelPath: req.GetString("model_path", ""), ModelSource: req.GetString("model_source", "")}
		},
		h.ModelValidate,
	))
}

func registerModelImpact(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("model_impact",
		mcpgo.WithDescription("What breaks if this component fails? Walks the model's failure graph from the component's failure states (all, or those named) and returns every component the failure reaches with a shortest chain, which of them are user-facing symptoms, the while: guards a chain depends on, and where a redundancy group stops the failure. From the model alone: no facts, no live system."),
		mcpgo.WithString("component", mcpgo.Required(), mcpgo.Description("the component to fail")),
		mcpgo.WithArray("states", mcpgo.WithStringItems(), mcpgo.Description("optional: only these failure states (types_describe lists them)")),
		mcpgo.WithString("model_path", mcpgo.Description("path to system.model.yaml on the server")),
		mcpgo.WithString("model_source", mcpgo.Description("the model YAML itself (max 512 KiB)")),
		rawOutput(ModelImpactOutputSchema),
	)
	s.AddTool(tool, dispatch("model_impact",
		func(req mcpgo.CallToolRequest) ModelImpactParams {
			return ModelImpactParams{
				ModelPath: req.GetString("model_path", ""), ModelSource: req.GetString("model_source", ""),
				Component: req.GetString("component", ""), States: req.GetStringSlice("states", nil),
			}
		},
		h.ModelImpact,
	))
}

func registerModelDiscover(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("model_discover",
		mcpgo.WithDescription("Run every installed provider's discovery -- this reads the live systems they cover -- and propose the model it implies: the proposed YAML, components added, discovered components no longer found (a real build refuses to drop them without --allow-deletes or --tombstone), authored components kept, dangling dependencies, and providers whose discovery failed. Merges with model_path when given, so hand-written components survive. Writes nothing; the proposal reaches the repository as a reviewed change. In the diagnose toolset, since it needs the providers' credentials."),
		mcpgo.WithString("model_path", mcpgo.Description("the existing model the proposal would replace, on the server")),
		mcpgo.WithArray("tombstone", mcpgo.WithStringItems(), mcpgo.Description("discovered components to keep although discovery no longer returns them")),
		rawOutput(ModelDiscoverOutputSchema),
	)
	s.AddTool(tool, dispatch("model_discover",
		func(req mcpgo.CallToolRequest) ModelDiscoverParams {
			return ModelDiscoverParams{ModelPath: req.GetString("model_path", ""), Tombstone: req.GetStringSlice("tombstone", nil)}
		},
		h.ModelDiscover,
	))
}

func registerModelDiff(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("model_diff",
		mcpgo.WithDescription("Compare two revisions of a model by what they mean: components added and removed; per component, dependency, effective health rule and var changes; and for each component whose failure now reaches different user-facing symptoms, which it gained or lost. Respelling rules or switching a bare healthy list to replace: with the same rules is no change. Each revision by path or inline. For reviewing a model change."),
		mcpgo.WithString("old_model_path", mcpgo.Description("the earlier revision, as a path on the server")),
		mcpgo.WithString("old_model_source", mcpgo.Description("the earlier revision's YAML (max 512 KiB)")),
		mcpgo.WithString("new_model_path", mcpgo.Description("the later revision, as a path on the server")),
		mcpgo.WithString("new_model_source", mcpgo.Description("the later revision's YAML (max 512 KiB)")),
		rawOutput(ModelDiffOutputSchema),
	)
	s.AddTool(tool, dispatch("model_diff",
		func(req mcpgo.CallToolRequest) ModelDiffParams {
			return ModelDiffParams{
				OldModelPath: req.GetString("old_model_path", ""), OldModelSource: req.GetString("old_model_source", ""),
				NewModelPath: req.GetString("new_model_path", ""), NewModelSource: req.GetString("new_model_source", ""),
			}
		},
		h.ModelDiff,
	))
}

func registerGuide(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("guide",
		mcpgo.WithDescription("Short notes on writing mgtt models and scenarios: the authoring loop, healthy: overrides (replace vs add), dependencies (redundancy groups vs while: guards), patterns from real models, and the scenario archetypes to write. Call with no topic for the index. Read model.health before overriding a type's rules, and scenarios.authoring before writing scenarios."),
		mcpgo.WithString("topic", mcpgo.Description("index (default), model.health, model.dependencies, model.patterns or scenarios.authoring")),
		rawOutput(GuideOutputSchema),
	)
	s.AddTool(tool, dispatch("guide",
		func(req mcpgo.CallToolRequest) GuideParams { return GuideParams{Topic: req.GetString("topic", "")} },
		h.Guide,
	))
}

func registerScenarioSimulate(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenario_simulate",
		mcpgo.WithDescription("Run scenarios against a model, as `mgtt simulate` does: each injects facts (inject:), optionally records probes that failed (unresolved: forbidden | transient | not_found), and states the conclusion expected (expect: root_cause, path, eliminated, not_eliminated, cannot_rule_out, redundancy_degraded). Returns pass/fail per scenario with expected beside actual. Model and scenarios go by path or inline; inline scenarios are YAML documents separated by ---. No probes, no live system, no writes."),
		mcpgo.WithString("model_path", mcpgo.Description("path to system.model.yaml on the server")),
		mcpgo.WithString("model_source", mcpgo.Description("the model YAML itself (max 512 KiB)")),
		mcpgo.WithString("scenarios_path", mcpgo.Description("a scenario file or a directory of them on the server")),
		mcpgo.WithString("scenarios_source", mcpgo.Description("scenario YAML itself; several separated by --- (max 512 KiB)")),
		rawOutput(ScenarioSimulateOutputSchema),
	)
	s.AddTool(tool, dispatch("scenario_simulate",
		func(req mcpgo.CallToolRequest) ScenarioSimulateParams {
			return ScenarioSimulateParams{
				ModelPath: req.GetString("model_path", ""), ModelSource: req.GetString("model_source", ""),
				ScenariosPath: req.GetString("scenarios_path", ""), ScenariosSource: req.GetString("scenarios_source", ""),
			}
		},
		h.ScenarioSimulate,
	))
}

// serverInstructions is the workflow an agent needs before its first call.
// Clients may drop it, so every step is also in the tool descriptions.
const serverInstructions = `mgtt diagnoses a running system against its committed model.
Workflow: incident_start with the model path, then loop: plan (what to check next and why), probe with execute=true (or fact_add for a fact you gathered yourself), until plan names a root cause or reports none. incident_snapshot summarises the state; incident_end closes it.
A probe that returns forbidden or transient recorded an unknown fact: the component stays a suspect, it is not cleared.
about reports the safety posture: read-only enforcement, the write-probe policy and the per-incident probe budget, and which toolset is served.
To write or change a model, start with guide (no topic): types_list and types_describe for the vocabulary (never invent fact names), then model_validate with the draft as model_source until it reports no errors, then scenario_simulate with scenarios that pin what the model must conclude. These tools read no live system and write nothing; the model reaches the repository as a reviewed change.`

// legacyToolNames maps each tool renamed in 0.4 to its old dotted name.
var legacyToolNames = map[string]string{
	"incident_start":    "incident.start",
	"incident_end":      "incident.end",
	"incident_snapshot": "incident.snapshot",
	"fact_add":          "fact.add",
	"facts_list":        "facts.list",
	"scenarios_list":    "scenarios.list",
	"scenarios_alive":   "scenarios.alive",
}

// registerLegacyAliases registers each dotted name as a copy of the tool
// it was renamed to, marked deprecated in its description.
func registerLegacyAliases(s *server.MCPServer) {
	for name, legacy := range legacyToolNames {
		t := s.GetTool(name)
		if t == nil {
			continue
		}
		alias := *t
		alias.Tool.Name = legacy
		alias.Tool.Description = "Deprecated alias of " + name + ", removed in the next minor release. " + t.Tool.Description
		s.AddTools(alias)
	}
}

// rawOutput wraps a schema constant as a json.RawMessage for
// mcpgo.WithRawOutputSchema. Wire-side agents that call tools/list see
// the returned shape, not an auto-derived approximation.
func rawOutput(s string) mcpgo.ToolOption {
	return mcpgo.WithRawOutputSchema(json.RawMessage(s))
}

// dispatch adapts a typed handler method into mcpgo's tool-handler shape.
// bind extracts typed params from the raw request; fn executes the method
// and returns a JSON-marshalable result. toolName appears only in the
// marshal-failure error message so operators can trace which tool tripped.
func dispatch[P any, R any](toolName string, bind func(mcpgo.CallToolRequest) P, fn func(P) (R, error)) server.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result, err := fn(bind(req))
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("%s: marshal result: %w", toolName, err)
		}
		return mcpgo.NewToolResultText(string(body)), nil
	}
}

// incidentIDOnly is the common binder for tools whose only input is
// `incident_id`. Generic over the concrete Params struct type; fn pulls
// the string from req and stuffs it into a zero-valued P via setter.
func incidentIDOnly[P any](set func(*P, string)) func(mcpgo.CallToolRequest) P {
	return func(req mcpgo.CallToolRequest) P {
		var p P
		set(&p, req.GetString("incident_id", ""))
		return p
	}
}

func registerAbout(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("about",
		mcpgo.WithDescription("Server metadata — version, active transports, current safety posture"),
		rawOutput(AboutOutputSchema),
	)
	s.AddTool(tool, dispatch("about",
		func(mcpgo.CallToolRequest) struct{} { return struct{}{} },
		func(struct{}) (*AboutResult, error) { return h.About() },
	))
}

func registerIncidentStart(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident_start",
		mcpgo.WithDescription("Create a new incident bound to a model. Returns incident_id for subsequent tool calls."),
		mcpgo.WithString("model_ref",
			mcpgo.Required(),
			mcpgo.Description("path to system.model.yaml the server can read"),
		),
		mcpgo.WithString("id",
			mcpgo.Description("optional incident id; generated if omitted"),
		),
		mcpgo.WithArray("suspect",
			mcpgo.Description(`optional component.state hints, e.g. ["api.crash_looping"]`),
		),
		rawOutput(IncidentStartOutputSchema),
	)
	s.AddTool(tool, dispatch("incident_start",
		func(req mcpgo.CallToolRequest) IncidentStartParams {
			return IncidentStartParams{
				ModelRef: req.GetString("model_ref", ""),
				ID:       req.GetString("id", ""),
				Suspect:  req.GetStringSlice("suspect", nil),
			}
		},
		h.IncidentStart,
	))
}

func registerIncidentEnd(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident_end",
		mcpgo.WithDescription("Close an incident. Persists the end timestamp and optional verdict; returns saved=true on success. With emit_scenario=true, also writes scenarios/<incident_id>.yaml next to the model -- the incident's facts and the engine's conclusion as an `mgtt simulate` regression test -- and returns scenario_path, scenario_yaml and scenario_passes (or scenario_warning when nothing was written: no facts, or the file already exists)."),
		mcpgo.WithString("incident_id",
			mcpgo.Required(),
			mcpgo.Description("id returned by incident_start"),
		),
		mcpgo.WithString("verdict",
			mcpgo.Description("optional human or agent note recording the conclusion"),
		),
		mcpgo.WithBoolean("emit_scenario",
			mcpgo.Description("write this incident as a simulate scenario next to the model; never overwrites an existing file"),
		),
		rawOutput(IncidentEndOutputSchema),
	)
	s.AddTool(tool, dispatch("incident_end",
		func(req mcpgo.CallToolRequest) IncidentEndParams {
			return IncidentEndParams{
				IncidentID:   req.GetString("incident_id", ""),
				Verdict:      req.GetString("verdict", ""),
				EmitScenario: req.GetBool("emit_scenario", false),
			}
		},
		h.IncidentEnd,
	))
}

func registerFactAdd(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("fact_add",
		mcpgo.WithDescription("Append an observation to an incident's fact store."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Required()),
		mcpgo.WithString("key", mcpgo.Required()),
		mcpgo.WithAny("value", mcpgo.Description("observed value — any JSON primitive, object, or array")),
		mcpgo.WithString("note", mcpgo.Description("optional note on provenance")),
		rawOutput(FactAddOutputSchema),
	)
	s.AddTool(tool, dispatch("fact_add",
		func(req mcpgo.CallToolRequest) FactAddParams {
			return FactAddParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
				Key:        req.GetString("key", ""),
				Value:      req.GetArguments()["value"], // WithAny — no typed getter
				Note:       req.GetString("note", ""),
			}
		},
		h.FactAdd,
	))
}

func registerFactsList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("facts_list",
		mcpgo.WithDescription("List facts recorded for an incident, optionally filtered to one component."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional component filter")),
		rawOutput(FactsListOutputSchema),
	)
	s.AddTool(tool, dispatch("facts_list",
		func(req mcpgo.CallToolRequest) FactsListParams {
			return FactsListParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
			}
		},
		h.FactsList,
	))
}

func registerPlan(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("plan",
		mcpgo.WithDescription("Compute the current path tree for an incident and suggest the next probe. Does not execute anything."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional entry point override")),
		rawOutput(PlanOutputSchema),
	)
	s.AddTool(tool, dispatch("plan",
		func(req mcpgo.CallToolRequest) PlanParams {
			return PlanParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
			}
		},
		h.Plan,
	))
}

func registerProbe(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("probe",
		mcpgo.WithDescription("Render or execute the engine's next suggested probe. execute=false is read-only (rendered command, no system touch); execute=true runs it and appends the resulting fact."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		mcpgo.WithString("component", mcpgo.Description("optional entry-point override")),
		mcpgo.WithBoolean("execute", mcpgo.Required(),
			mcpgo.Description("when false the probe is rendered but not run")),
		rawOutput(ProbeOutputSchema),
	)
	s.AddTool(tool, dispatch("probe",
		func(req mcpgo.CallToolRequest) ProbeParams {
			return ProbeParams{
				IncidentID: req.GetString("incident_id", ""),
				Component:  req.GetString("component", ""),
				Execute:    req.GetBool("execute", false),
			}
		},
		h.Probe,
	))
}

func registerScenariosList(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios_list",
		mcpgo.WithDescription("Enumerate every failure chain the engine considers for this incident's model."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, dispatch("scenarios_list",
		incidentIDOnly(func(p *ScenariosListParams, id string) { p.IncidentID = id }),
		h.ScenariosList,
	))
}

func registerScenariosAlive(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("scenarios_alive",
		mcpgo.WithDescription("Subset of enumerated scenarios still consistent with the incident's facts."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(ScenariosListOutputSchema),
	)
	s.AddTool(tool, dispatch("scenarios_alive",
		incidentIDOnly(func(p *ScenariosAliveParams, id string) { p.IncidentID = id }),
		h.ScenariosAlive,
	))
}

func registerIncidentSnapshot(s *server.MCPServer, h *Handler) {
	tool := mcpgo.NewTool("incident_snapshot",
		mcpgo.WithDescription("Export an incident's full diagnostic memory — surviving and eliminated scenarios, facts, current suggestion, status."),
		mcpgo.WithString("incident_id", mcpgo.Required()),
		rawOutput(IncidentSnapshotOutputSchema),
	)
	s.AddTool(tool, dispatch("incident_snapshot",
		incidentIDOnly(func(p *IncidentSnapshotParams, id string) { p.IncidentID = id }),
		h.IncidentSnapshot,
	))
}

// runHTTP serves the MCP endpoint until SIGINT / SIGTERM, then gracefully
// drains in-flight requests for up to shutdownGrace before returning.
func runHTTP(s *server.MCPServer, cfg Config) error {
	token, err := resolveToken(cfg)
	if err != nil {
		return err
	}
	httpSrv := buildHTTPServer(s, cfg, token)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.ListenAndServe() }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		return gracefulShutdown(httpSrv, cfg, serveErr)
	}
}

// buildHTTPServer composes the bearer-auth-wrapped streamable handler
// with a hardened http.Server (slow-loris protection via
// ReadHeaderTimeout, header-bomb protection via MaxHeaderBytes, idle
// budget so keep-alive goroutines don't pin).
func buildHTTPServer(s *server.MCPServer, cfg Config, token string) *http.Server {
	addr := cfg.Listen
	if addr == "" {
		addr = ":8080"
	}
	streamable := server.NewStreamableHTTPServer(s)
	return &http.Server{
		Addr:              addr,
		Handler:           withBearerAuth(token, streamable),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // streaming can exceed probe timeout; ctx handles it
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 14,
	}
}

// gracefulShutdown runs httpSrv.Shutdown under the configured drain
// budget, then waits for the serve goroutine to actually return so the
// listener is truly gone when runHTTP exits.
func gracefulShutdown(httpSrv *http.Server, cfg Config, serveErr <-chan error) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace(cfg))
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http serve goroutine: %w", err)
	}
	return nil
}

// shutdownGrace bounds how long runHTTP waits for in-flight requests to
// drain after SIGINT/SIGTERM. One in-flight probe's timeout plus a 5s
// envelope; capped to 5 minutes so a misconfigured probe_timeout does not
// hang SIGTERM indefinitely.
func shutdownGrace(cfg Config) time.Duration {
	probe := cfg.ProbeTimeoutSeconds
	if probe <= 0 {
		probe = 30
	}
	if probe > probeTimeoutSecondsMax {
		probe = probeTimeoutSecondsMax
	}
	return time.Duration(probe+5) * time.Second
}
