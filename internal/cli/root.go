package cli

import (
	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "mgtt",
	Short: "Model Guided Troubleshooting Tool",
}

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("mgtt version " + version)
		},
	})
}

func Execute() error {
	return rootCmd.Execute()
}

func RootCmd() *cobra.Command {
	return rootCmd
}
