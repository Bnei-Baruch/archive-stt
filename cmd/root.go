// Package cmd: the command line (cobra) and the settings (viper): config.toml and/or ARCHIVE_STT_* variables,
// a key "ai-trns.url" is ARCHIVE_STT_AI_TRNS_URL.
package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var RootCmd = &cobra.Command{
	Use:   "archive-stt",
	Short: "Lesson markup: chapters, paragraphs, speakers on the lesson's time line",
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	RootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default is config.toml, optional)")
	for k, v := range defaults {
		viper.SetDefault(k, v)
	}
}

// defaults: every setting, so each can come from the environment alone
var defaults = map[string]interface{}{
	"server.bind-address": ":8770",
	"server.api-keys":     "", // who may start a run, "name:key,..."; empty = only from this host
	"server.tick":         "10s",
	"server.log-level":    "info",
	"ai-trns.url":         "http://127.0.0.1:8766",
	"ai-trns.key":         "",
	"ai-trns.timeout":     "60s",
	"kmedia.url":          "https://kabbalahmedia.info",
	"kmedia.cdn-url":      "https://cdn.kabbalahmedia.info",
	"kmedia.timeout":      "30s",
	"data.dir":            "./data",
	"mdb.url":             "", // read only; empty = the words always from the STT
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
	}
	viper.SetEnvPrefix("ARCHIVE_STT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if cfgFile != "" || !errors.As(err, &notFound) {
			fmt.Fprintln(os.Stderr, "config:", err)
			os.Exit(1)
		}
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(viper.GetString("server.log-level"))); err != nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// apiKeys parses "name:key,name:key"
func apiKeys(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		name, key, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if ok && name != "" && key != "" {
			out[name] = key
		}
	}
	return out
}
