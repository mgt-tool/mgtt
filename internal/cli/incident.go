package cli

import (
	"fmt"
	"io"
	"sort"

	"mgtt/internal/facts"
	"mgtt/internal/incident"
	"mgtt/internal/model"

	"github.com/spf13/cobra"
)

var incidentCmd = &cobra.Command{
	Use:   "incident",
	Short: "Manage troubleshooting incidents",
}

var incidentStartID string
var incidentModelPath string

var incidentStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new incident",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := model.Load(incidentModelPath)
		if err != nil {
			return fmt.Errorf("load model: %w", err)
		}

		inc, err := incident.Start(m.Meta.Name, m.Meta.Version, incidentStartID)
		if err != nil {
			return err
		}

		renderIncidentStart(cmd.OutOrStdout(), inc)
		return nil
	},
}

var incidentEndCmd = &cobra.Command{
	Use:   "end",
	Short: "End the current incident",
	RunE: func(cmd *cobra.Command, args []string) error {
		inc, err := incident.End()
		if err != nil {
			return err
		}

		renderIncidentEnd(cmd.OutOrStdout(), inc, inc.Store)
		return nil
	},
}

func init() {
	incidentStartCmd.Flags().StringVar(&incidentStartID, "id", "", "incident ID (auto-generated if empty)")
	incidentStartCmd.Flags().StringVar(&incidentModelPath, "model", "system.model.yaml", "path to system.model.yaml")

	incidentCmd.AddCommand(incidentStartCmd)
	incidentCmd.AddCommand(incidentEndCmd)
	rootCmd.AddCommand(incidentCmd)
}

// renderIncidentStart renders a confirmation that an incident has been started.
func renderIncidentStart(w io.Writer, inc *incident.Incident) {
	fmt.Fprintf(w, "  %s %s started\n", checkmark(true), inc.ID)
	fmt.Fprintf(w, "    model: %s v%s\n", inc.Model, inc.Version)
	fmt.Fprintf(w, "    state: %s\n", inc.StateFile)
}

// renderIncidentEnd renders an incident closure summary.
func renderIncidentEnd(w io.Writer, inc *incident.Incident, store *facts.Store) {
	duration := inc.Ended.Sub(inc.Started)
	fmt.Fprintf(w, "  %s %s ended\n", checkmark(true), inc.ID)
	fmt.Fprintf(w, "    duration: %s\n", duration.Round(1e9)) // round to seconds

	// Count facts.
	components := store.AllComponents()
	sort.Strings(components)
	total := 0
	for _, c := range components {
		total += len(store.FactsFor(c))
	}
	fmt.Fprintf(w, "    facts:    %s across %s\n",
		pluralize(total, "fact", "facts"),
		pluralize(len(components), "component", "components"),
	)
}
