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
		check      bool
		testVoice  bool
	)
	flag.StringVar(&configPath, "config", "tgpager.yml", "Path to the configuration file")
	flag.BoolVar(&login, "login", false, "Authenticate interactively, write the session file and exit")
	flag.BoolVar(&check, "check", false, "Verify configuration and the speech provider, then exit")
	flag.BoolVar(&testVoice, "voice", false, "Send one test voice message to the peer and exit")
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

	if testVoice {
		if err := runVoice(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if check {
		if err := runCheck(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Configuration and speech provider are healthy")
		return
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

	callClient := tgcall.New(cfg.Telegram.AppID, cfg.Telegram.AppHash.Value, cfg.Telegram.Session,
		tgcall.WithLogger(lg),
		tgcall.WithTracerProvider(t.TracerProvider()),
		tgcall.WithMeterProvider(t.MeterProvider()),
		tgcall.WithPeer(cfg.Peer),
		tgcall.WithBotToken(cfg.Telegram.BotToken.Value),
		tgcall.WithPeerStorage(peerStorage),
		tgcall.WithRingTimeout(cfg.Call.RingTimeout),
		tgcall.WithConnectTimeout(cfg.Call.ConnectTimeout),
		tgcall.WithRetry(cfg.Call.Attempts, cfg.Call.RetryDelay),
		tgcall.WithVoiceRetry(cfg.Voice.Attempts, cfg.Voice.RetryDelay),
	)

	token := cfg.Webhook.Token.Value
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

	// Warming runs alongside serving, never before it. A provider that is down
	// must not keep the pager from starting: the outage that broke it is
	// exactly what still needs paging about.
	g.Go(func() error {
		if err := speaker.Preflight(ctx); err != nil {
			lg.Warn("Speech provider is unhealthy, pages will play the tone alone",
				zap.Error(err))
			return nil
		}
		return nil
	})

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
					processPage(ctx, lg, callClient, streamer, speaker, cfg.Voice, req)
					srv.Done(req.GroupKey)
				}
			}
		})
	})

	return g.Wait()
}

// runCheck exercises the speech path once and reports, for a deploy pipeline
// that wants to fail before shipping rather than discover it at 3am.
func runCheck(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if _, err := os.Stat(cfg.Audio); err != nil {
		return errors.Wrap(err, "audio file")
	}

	lg, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	speaker, err := tts.Build(cfg, tts.BuildOptions{Logger: lg, Tone: cfg.Audio})
	if err != nil {
		return errors.Wrap(err, "build speaker")
	}
	return speaker.Preflight(ctx)
}

// runVoice sends a single voice message and exits, so the message path can be
// exercised without waiting for an alert or ringing anybody. It ignores
// voice.mode: asking for it is the request.
func runVoice(cfg config.Config) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	lg, err := zap.NewDevelopment()
	if err != nil {
		return err
	}

	speaker, err := tts.Build(cfg, tts.BuildOptions{Logger: lg, Tone: cfg.Audio})
	if err != nil {
		return errors.Wrap(err, "build speaker")
	}

	client := tgcall.New(cfg.Telegram.AppID, cfg.Telegram.AppHash.Value, cfg.Telegram.Session,
		tgcall.WithLogger(lg),
		tgcall.WithPeer(cfg.Peer),
		tgcall.WithBotToken(cfg.Telegram.BotToken.Value),
		tgcall.WithVoiceRetry(cfg.Voice.Attempts, cfg.Voice.RetryDelay),
	)
	return client.Run(ctx, func(ctx context.Context) error {
		spec := speaker.Speak(ctx, tts.PreflightPayload())
		return sendVoice(ctx, lg, client, audio.NewFFmpeg(), spec, cfg.Voice)
	})
}

// runLogin authenticates outside app.Run, so the terminal prompts are not
// interleaved with server logs.
func runLogin(tg config.Telegram) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	client := tgcall.New(tg.AppID, tg.AppHash.Value, tg.Session)
	if err := client.AuthFlow(ctx); err != nil {
		return errors.Wrap(err, "authenticate")
	}
	fmt.Fprintf(os.Stderr, "Authenticated, session written to %s\n", tg.Session)
	return nil
}

func processPage(
	ctx context.Context,
	lg *zap.Logger,
	client *tgcall.Client,
	streamer *audio.FFmpegStreamer,
	speaker *tts.Speaker,
	voice config.Voice,
	req server.CallRequest,
) {
	lg = lg.With(zap.String("groupKey", req.GroupKey))

	// Synthesized before the call is placed: doing it after connect would play
	// the callee an HTTP round trip of silence.
	spec := speaker.Speak(ctx, req.Payload)

	var callErr error
	if voice.Mode.Calls() {
		callErr = placeCall(ctx, lg, client, streamer, spec)
		if callErr != nil {
			lg.Error("Failed to page", zap.Error(callErr))
		} else {
			lg.Info("Paged successfully")
		}
	}

	if !voice.Mode.Sends(callErr != nil) {
		return
	}
	if err := sendVoice(ctx, lg, client, streamer, spec, voice); err != nil {
		// Only in "only" mode is this the page itself failing; everywhere else
		// a call has already been placed and this is the record of it.
		if voice.Mode == config.VoiceOnly {
			lg.Error("Failed to page", zap.Error(err))
			return
		}
		lg.Warn("Failed to leave a voice message", zap.Error(err))
		return
	}
	lg.Info("Left a voice message")
}

func placeCall(ctx context.Context, lg *zap.Logger, client *tgcall.Client, streamer *audio.FFmpegStreamer, spec audio.Spec) error {
	return client.Ring(ctx, func(ctx context.Context, call *tgcall.Call) error {
		lg.Info("Streaming audio",
			zap.Strings("segments", spec.Segments),
			zap.Int("repeat", spec.Repeat),
		)
		return streamer.Stream(ctx, call.WriteRTP, spec, audio.WithLogger(lg))
	})
}

func sendVoice(ctx context.Context, lg *zap.Logger, client *tgcall.Client, renderer audio.Renderer, spec audio.Spec, voice config.Voice) error {
	ctx, cancel := context.WithTimeout(ctx, voice.Timeout)
	defer cancel()

	f, err := os.CreateTemp("", "tgpager-*.ogg")
	if err != nil {
		return errors.Wrap(err, "create temp file")
	}
	path := f.Name()
	// ffmpeg writes the file itself; the handle only reserved the name.
	if err := f.Close(); err != nil {
		return errors.Wrap(err, "close temp file")
	}
	defer func() {
		if err := os.Remove(path); err != nil {
			lg.Warn("Failed to remove rendered voice message", zap.Error(err))
		}
	}()

	if err := renderer.Render(ctx, voiceSpec(spec), path, audio.WithLogger(lg)); err != nil {
		return errors.Wrap(err, "render")
	}
	dur, err := audio.OggDuration(path)
	if err != nil {
		return errors.Wrap(err, "read duration")
	}
	return client.SendVoice(ctx, path, dur)
}

// voiceSpec is the speech alone, once.
//
// The call plays tone, speech, tone, speech: the tone wakes someone and the
// repeat catches a callee answering mid-sentence. Neither applies to a chat
// message, where the notification is the attention-getter and replay is a tap.
// Speech is the last segment, and is the tone itself when synthesis was
// unavailable, which is still worth recording.
func voiceSpec(spec audio.Spec) audio.Spec {
	return audio.File(spec.Segments[len(spec.Segments)-1])
}
