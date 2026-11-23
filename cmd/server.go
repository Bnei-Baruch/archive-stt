package cmd

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Bnei-Baruch/archive-stt/aitrns"
	"github.com/Bnei-Baruch/archive-stt/api"
	"github.com/Bnei-Baruch/archive-stt/kmedia"
	"github.com/Bnei-Baruch/archive-stt/pipeline"
	"github.com/Bnei-Baruch/archive-stt/store"
	"github.com/Bnei-Baruch/archive-stt/utils"
	"github.com/Bnei-Baruch/archive-stt/version"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "The API server and the loop that moves pending runs on",
	Run:   serverFn,
}

func init() {
	RootCmd.AddCommand(serverCmd)
}

// NewPipeline is the pipeline on the configured store and services
func NewPipeline() (*pipeline.Pipeline, error) {
	s, err := store.Open(viper.GetString("data.dir"))
	if err != nil {
		return nil, err
	}
	return &pipeline.Pipeline{
		Store:  s,
		AiTrns: aitrns.New(viper.GetString("ai-trns.url"), viper.GetString("ai-trns.key"), viper.GetDuration("ai-trns.timeout")),
		Kmedia: kmedia.New(viper.GetString("kmedia.url"), viper.GetString("kmedia.cdn-url"), viper.GetDuration("kmedia.timeout")),
	}, nil
}

func serverFn(cmd *cobra.Command, args []string) {
	slog.Info("starting archive-stt", "version", version.Version)
	p, err := NewPipeline()
	utils.Must(err)
	defer p.Store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// the maintenance loop: pending runs move on even when nobody asks about them
	go func() {
		t := time.NewTicker(viper.GetDuration("server.tick"))
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.SyncAll()
			}
		}
	}()

	srv := &http.Server{
		Addr:              viper.GetString("server.bind-address"),
		Handler:           api.Handler(&api.Handlers{Pipeline: p, APIKeys: apiKeys(viper.GetString("server.api-keys"))}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
}
