// Package audio encodes an audio file into Opus RTP packets.
package audio

import (
	"context"

	"github.com/pion/rtp"
)

type Streamer interface {
	Stream(ctx context.Context, write func(*rtp.Packet) error, file string, opts ...StreamOption) error
}

type FFmpegStreamer struct {
	ffmpegPath string
}

func NewFFmpeg() *FFmpegStreamer {
	return &FFmpegStreamer{ffmpegPath: "ffmpeg"}
}

func FFmpegWithPath(path string) *FFmpegStreamer {
	return &FFmpegStreamer{ffmpegPath: path}
}
