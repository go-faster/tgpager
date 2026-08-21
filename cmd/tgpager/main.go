package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-faster/errors"
	"github.com/go-faster/sdk/app"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/go-faster/tgpager/internal/audio"
	"github.com/go-faster/tgpager/internal/config"
	"github.com/go-faster/tgpager/internal/peercache"
	"github.com/go-faster/tgpager/internal/server"
	"github.com/go-faster/tgpager/internal/tgcall"
	"github.com/go-faster/tgpager/internal/tts"
)

func main() {
	var (
		configPath string
		login      bool
	)
	flag.StringVar(&configPath, "config", "tgpager.yml", "Path to the configuration file")
	flag.BoolVar(&login, "login", false, "Authenticate interactively, write the session file and exit")
	flag.Parse()

	cfg, _, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if login {
		if err := runLogin(cfg.Telegram); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Checked up front: otherwise a bad path only surfaces after a real call
	// has been placed and answered.
	if _, err := os.Stat(cfg.Audio); err != nil {
		fmt.Fprintf(os.Stderr, "audio file: %v\n", err)
		os.Exit(1)
	}

	zapCfg := zap.NewProductionConfig()
	if cfg.Debug {
		zapCfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		zapCfg.EncoderConfig.TimeKey = zapcore.OmitKey
	}

	app.Run(func(ctx context.Context, lg *zap.Logger, t *app.Telemetry) error {
		return run(ctx, lg, t, cfg)
	}, app.WithZapConfig(zapCfg))
}

func run(ctx context.Context, lg *zap.Logger, t *app.Telemetry, cfg config.Config) error {
	peerStorage, err := peercache.Open(cfg.PeerCache)
	if err != nil {
		return errors.Wrap(err, "open peer cache")
	}
	defer func() {
		if err := peerStorage.Close(); err != nil {
			lg.Error("Failed to close peer cache", zap.Error(err))
		}
	}()

	callClient := tgcall.New(cfg.Telegram.AppID, cfg.Telegram.AppHash, cfg.Telegram.Session,
		tgcall.WithLogger(lg),
		tgcall.WithTracerProvider(t.TracerProvider()),
		tgcall.WithMeterProvider(t.MeterProvider()),
		tgcall.WithPeer(cfg.Peer),
		tgcall.WithPeerStorage(peerStorage),
		tgcall.WithRingTimeout(cfg.Call.RingTimeout),
		tgcall.WithConnectTimeout(cfg.Call.ConnectTimeout),
		tgcall.WithRetry(cfg.Call.Attempts, cfg.Call.RetryDelay),
	)

	token, _ := cfg.Webhook.Token.Value()
	srv := server.New(cfg.Webhook.QueueSize,
		server.WithLogger(lg),
		server.WithToken(token),
		server.WithMeterProvider(t.MeterProvider()),
	)

	httpServer := &http.Server{
		Addr:              cfg.Webhook.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	speaker, err := tts.Build(cfg, tts.BuildOptions{
		Logger:         lg,
		TracerProvider: t.TracerProvider(),
		MeterProvider:  t.MeterProvider(),
		Tone:           cfg.Audio,
	})
	if err != nil {
		return errors.Wrap(err, "build speaker")
	}

	streamer := audio.NewFFmpeg()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		lg.Info("Serving webhooks", zap.String("addr", cfg.Webhook.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.Wrap(err, "serve http")
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		// Shutdown needs a live context, the group's is already canceled.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return errors.Wrap(err, "shutdown http")
		}
		return nil
	})

	g.Go(func() error {
		return callClient.Run(ctx, func(ctx context.Context) error {
			lg.Info("Telegram client ready")

			for {
				select {
				case <-ctx.Done():
					return nil
				case req := <-srv.Queue():
					processCall(ctx, lg, callClient, streamer, speaker, req)
					srv.Done(req.GroupKey)
				}
			}
		})
	})

	return g.Wait()
}

// runLogin authenticates outside app.Run, so the terminal prompts are not
// interleaved with server logs.
func runLogin(tg config.Telegram) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	client := tgcall.New(tg.AppID, tg.AppHash, tg.Session)
	if err := client.AuthFlow(ctx); err != nil {
		return errors.Wrap(err, "authenticate")
	}
	fmt.Fprintf(os.Stderr, "Authenticated, session written to %s\n", tg.Session)
	return nil
}

func processCall(ctx context.Context, lg *zap.Logger, client *tgcall.Client, streamer audio.Streamer, speaker *tts.Speaker, req server.CallRequest) {
	lg = lg.With(zap.String("groupKey", req.GroupKey))

	// Synthesized before the call is placed: doing it after connect would play
	// the callee an HTTP round trip of silence.
	spec := speaker.Speak(ctx, req.Payload)

	err := client.Ring(ctx, func(ctx context.Context, call *tgcall.Call) error {
		lg.Info("Streaming audio",
			zap.Strings("segments", spec.Segments),
			zap.Int("repeat", spec.Repeat),
		)
		return streamer.Stream(ctx, call.WriteRTP, spec, audio.WithLogger(lg))
	})
	if err != nil {
		lg.Error("Failed to page", zap.Error(err))
		return
	}
	lg.Info("Paged successfully")
}
