package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var outFile string

const template = `# archive-stt config; every key can also come from the environment: ai-trns.url -> ARCHIVE_STT_AI_TRNS_URL
[server]
bind-address=":8770"
api-keys=""          # who may start a run, "name:key,..."; empty = only from this host (reading is public)
tick="10s"           # how often pending runs are moved on
log-level="info"     # debug | info | warn | error

[ai-trns]
url="http://127.0.0.1:8766"
key=""               # a service key of ai-trns (its AI_TRNS_API_KEYS)
timeout="60s"

[kmedia]
url="https://kabbalahmedia.info"
cdn-url="https://cdn.kabbalahmedia.info"   # the audio for ai-trns (the host must be in its AI_TRNS_ALLOWED_URL_HOSTS)
timeout="30s"

[mdb]
url=""               # MDB, read only (postgres://...): a lesson's checked subtitles; empty = words from the STT only

[data]
dir="./data"         # SQLite and the words files; nothing is removed

[markup]
ssh=""               # archive-stt markup: the server for the SSH tunnel, user@host
`

var configCmd = &cobra.Command{
	Use:   "config [file]",
	Short: "Write a configuration template to a file or stdout",
	Run:   configFn,
}

func init() {
	RootCmd.AddCommand(configCmd)
	configCmd.Flags().StringVarP(&outFile, "file", "f", "", "the file to write (default: stdout)")
}

func configFn(cmd *cobra.Command, args []string) {
	if outFile == "" && len(args) > 0 {
		outFile = args[0]
	}
	if outFile == "" {
		fmt.Print(template)
		return
	}
	if err := os.WriteFile(outFile, []byte(template), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
