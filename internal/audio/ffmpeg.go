package audio

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os/exec"
	"time"

	"github.com/go-faster/errors"
	"github.com/pion/rtp"
	"go.uber.org/zap"
)

const (
	opusPayloadType = 111
	sampleRate      = 48000
	frameDurationMs = 20
)

type streamOptions struct {
	logger *zap.Logger
}

type StreamOption func(*streamOptions)

func WithLogger(lg *zap.Logger) StreamOption {
	return func(o *streamOptions) {
		o.logger = lg
	}
}

func (f *FFmpegStreamer) Stream(ctx context.Context, write func(*rtp.Packet) error, file string, opts ...StreamOption) error {
	o := &streamOptions{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(o)
	}
	lg := o.logger
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP: net.ParseIP("127.0.0.1"),
	})
	if err != nil {
		return errors.Wrap(err, "listen rtp udp")
	}
	defer func() { _ = conn.Close() }()

	local := conn.LocalAddr().(*net.UDPAddr)
	rng := rand.New(rand.NewSource(rand.Int63()))
	ssrc := rng.Uint32()

	args := []string{
		"-re",
		"-i", file,
		"-vn",
		"-c:a", "libopus",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", "1",
		"-frame_duration", fmt.Sprintf("%d", frameDurationMs),
		"-payload_type", fmt.Sprintf("%d", opusPayloadType),
		"-ssrc", fmt.Sprintf("%d", ssrc),
		"-f", "rtp",
		fmt.Sprintf("rtp://%s:%d", local.IP.String(), local.Port),
	}

	lg.Debug("Starting ffmpeg", zap.String("path", f.ffmpegPath), zap.Strings("args", args))

	cmd := exec.CommandContext(ctx, f.ffmpegPath, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrap(err, "stderr pipe")
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "start ffmpeg")
	}

	stderrCh := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(stderr)
		stderrCh <- data
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	buf := make([]byte, 4096)

	for {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			_ = cmd.Process.Kill()
			return errors.Wrap(err, "set read deadline")
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				select {
				case <-ctx.Done():
					_ = cmd.Process.Kill()
					return ctx.Err()
				case waitErr := <-waitCh:
					return finishFFmpeg(waitErr, stderrCh, lg)
				default:
					continue
				}
			}
			_ = cmd.Process.Kill()
			return errors.Wrap(err, "read rtp packet")
		}

		var pkt rtp.Packet
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			_ = cmd.Process.Kill()
			return errors.Wrap(err, "decode rtp packet")
		}

		lg.Debug("Sending RTP packet",
			zap.Uint16("seq", pkt.SequenceNumber),
			zap.Uint32("ts", pkt.Timestamp),
			zap.Int("payload_len", n),
		)

		if err := write(&pkt); err != nil {
			_ = cmd.Process.Kill()
			return errors.Wrap(err, "write rtp")
		}
	}
}

func finishFFmpeg(waitErr error, stderrCh <-chan []byte, lg *zap.Logger) error {
	stderr := <-stderrCh
	if len(stderr) > 0 {
		lg.Debug("ffmpeg stderr", zap.ByteString("output", stderr))
	}
	if waitErr != nil {
		return errors.Wrap(waitErr, "ffmpeg")
	}
	return nil
}
