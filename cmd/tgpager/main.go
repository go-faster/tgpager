package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-faster/sdk/app"
	"github.com/go-faster/tgpager/internal/audio"
	"github.com/go-faster/tgpager/internal/server"
	"github.com/go-faster/tgpager/internal/tgcall"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	var (
		addr           string
		sessionFile    string
		appID          int
		appHash        string
		peer           string
		audioFile      string
		ringTimeout    time.Duration
		connectTimeout time.Duration
		attempts       int
		retryDelay     time.Duration
		debug          bool
	)

	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&sessionFile, "session", "session.json", "Telegram session file path")
	flag.IntVar(&appID, "app-id", 0, "Telegram app ID")
	flag.StringVar(&appHash, "app-hash", "", "Telegram app hash")
	flag.StringVar(&peer, "peer", "", "Call target: @username, phone number or t.me link")
	flag.StringVar(&audioFile, "audio", "", "Path to audio file to play during call")
	flag.DurationVar(&ringTimeout, "ring-timeout", 45*time.Second, "How long an unanswered call keeps ringing")
	flag.DurationVar(&connectTimeout, "connect-timeout", 30*time.Second, "How long an accepted call may take to negotiate media")
	flag.IntVar(&attempts, "attempts", 3, "How many times to place a call before giving up")
	flag.DurationVar(&retryDelay, "retry-delay", 10*time.Second, "Delay between call attempts")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.Parse()
	if appID == 0 {
		if envAppID := os.Getenv("APP_ID"); envAppID != "" {
			parsed, err := strconv.Atoi(envAppID)
			if err != nil {
				fmt.Fprintln(os.Stderr, "APP_ID must be an integer")
				os.Exit(1)
			}
			appID = parsed
		}
	}
	if appHash == "" {
		appHash = os.Getenv("APP_HASH")
	}

	if appID == 0 || appHash == "" {
		fmt.Fprintln(os.Stderr, "app-id/app-hash or APP_ID/APP_HASH are required")
		os.Exit(1)
	}
	if peer == "" {
		peer = os.Getenv("PEER")
	}
	if peer == "" {
		fmt.Fprintln(os.Stderr, "peer is required")
		os.Exit(1)
	}
	if audioFile == "" {
		fmt.Fprintln(os.Stderr, "audio is required")
		os.Exit(1)
	}

	cfg := zap.NewProductionConfig()
	if debug {
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		cfg.EncoderConfig.TimeKey = zapcore.OmitKey
	}

	app.Run(func(ctx context.Context, lg *zap.Logger, t *app.Telemetry) error {
		callClient := tgcall.New(appID, appHash, sessionFile,
			tgcall.WithLogger(lg),
			tgcall.WithPeer(peer),
			tgcall.WithRingTimeout(ringTimeout),
			tgcall.WithConnectTimeout(connectTimeout),
			tgcall.WithRetry(attempts, retryDelay),
		)

		srv := server.New(100, server.WithLogger(lg))

		httpServer := &http.Server{
			Addr:    addr,
			Handler: srv,
		}

		streamer := audio.NewFFmpeg()

		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				lg.Fatal("HTTP server failed", zap.Error(err))
			}
		}()

		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				lg.Error("HTTP server shutdown error", zap.Error(err))
			}
		}()

		return callClient.Run(ctx, func(ctx context.Context) error {
			lg.Info("Telegram client ready")

			for {
				select {
				case <-ctx.Done():
					return nil
				case req := <-srv.Queue():
					lg.Info("Processing call request", zap.String("groupKey", req.GroupKey))
					processCall(ctx, lg, callClient, streamer, audioFile, req)
					srv.Done(req.GroupKey)
				}
			}
		})
	}, app.WithZapConfig(cfg))
}

func processCall(ctx context.Context, lg *zap.Logger, client *tgcall.Client, streamer audio.Streamer, audioFile string, req server.CallRequest) {
	lg = lg.With(zap.String("groupKey", req.GroupKey))

	err := client.Ring(ctx, func(ctx context.Context, call *tgcall.Call) error {
		lg.Info("Streaming audio", zap.String("file", audioFile))
		return streamer.Stream(ctx, call.WriteRTP, audioFile, audio.WithLogger(lg))
	})
	if err != nil {
		lg.Error("Failed to page", zap.Error(err))
		return
	}
	lg.Info("Paged successfully")
}
