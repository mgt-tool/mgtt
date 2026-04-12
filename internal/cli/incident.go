package cli

import (
	"fmt"

	"mgtt/internal/incident"
	"mgtt/internal/model"
	"mgtt/internal/render"

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

		render.IncidentStart(cmd.OutOrStdout(), inc)
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

		render.IncidentEnd(cmd.OutOrStdout(), inc, inc.Store)
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
