package cmd

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// health is the container's health check: the image has no curl
var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check that the server on server.bind-address answers /health",
	Run: func(cmd *cobra.Command, args []string) {
		_, port, err := net.SplitHostPort(viper.GetString("server.bind-address"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		client := &http.Client{Timeout: 4 * time.Second}
		res, err := client.Get("http://127.0.0.1:" + port + "/health")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			fmt.Fprintln(os.Stderr, res.Status)
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(healthCmd)
}
