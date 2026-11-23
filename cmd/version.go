package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Bnei-Baruch/archive-stt/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("archive-stt %s\n", version.Version)
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
