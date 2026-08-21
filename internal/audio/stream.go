// Package audio encodes audio files into a single continuous Opus RTP stream.
package audio

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/pion/rtp"
)

// Spec describes what a call plays: the segments in order, repeated as a whole.
//
// Everything is composed inside one ffmpeg process. Streaming segments with
// separate invocations would restart RTP sequence numbers and timestamps mid
// call, which a receiver may treat as a broken stream.
type Spec struct {
	Segments []string
	Repeat   int
}

// File is a Spec that plays one file once.
func File(path string) Spec {
	return Spec{Segments: []string{path}, Repeat: 1}
}

// Validate reports whether the spec describes something playable.
func (s Spec) Validate() error {
	if len(s.Segments) == 0 {
		return errors.New("no segments to play")
	}
	for i, seg := range s.Segments {
		if seg == "" {
			return errors.Errorf("segment %d has no path", i)
		}
	}
	if s.Repeat < 1 {
		return errors.Errorf("repeat must be at least 1, got %d", s.Repeat)
	}
	return nil
}

type Streamer interface {
	Stream(ctx context.Context, write func(*rtp.Packet) error, spec Spec, opts ...StreamOption) error
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
