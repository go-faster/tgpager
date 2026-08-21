package audio

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"time"

	"github.com/go-faster/errors"
)

const (
	// oggPageHeader is the fixed part of an Ogg page header, before the
	// segment table.
	oggPageHeaderSize = 27
	// opusHeadSize is the fixed part of an OpusHead identification header.
	opusHeadSize = 19
	// oggHeadWindow bounds the search for OpusHead, which the format requires
	// to be alone in the first page.
	oggHeadWindow = 4096
	// oggTailWindow bounds the search backwards for the final page. A page
	// holds at most 255 segments of 255 bytes.
	oggTailWindow = 128 << 10
)

var oggMagic = []byte("OggS")

// OggDuration reports the playable length of an Ogg/Opus file.
//
// Telegram renders a voice message as 0:00 without a duration, and the length
// is only known after encoding: it is however long the concatenated inputs
// came out. Rather than shell out to ffprobe, ask the file. The final page's
// granule position counts samples at 48kHz, and OpusHead carries the pre-skip
// to subtract.
func OggDuration(path string) (time.Duration, error) {
	// #nosec G304 -- the path is a file this process just rendered.
	f, err := os.Open(path)
	if err != nil {
		return 0, errors.Wrap(err, "open")
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return 0, errors.Wrap(err, "stat")
	}
	size := info.Size()

	head := make([]byte, min(size, oggHeadWindow))
	if _, err := io.ReadFull(f, head); err != nil {
		return 0, errors.Wrap(err, "read header")
	}
	preSkip, err := opusPreSkip(head)
	if err != nil {
		return 0, err
	}

	tail := make([]byte, min(size, oggTailWindow))
	if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
		return 0, errors.Wrap(err, "read tail")
	}
	granule, err := lastGranule(tail)
	if err != nil {
		return 0, err
	}

	samples := granule - int64(preSkip)
	if samples <= 0 {
		return 0, nil
	}
	return samplesDuration(samples)
}

// samplesDuration converts a 48kHz sample count without overflowing.
//
// A granule position is whatever the file says it is, and scaling one straight
// to nanoseconds overflows int64 at around 53 hours of audio, wrapping to a
// negative duration.
func samplesDuration(samples int64) (time.Duration, error) {
	seconds, remainder := samples/sampleRate, samples%sampleRate
	if seconds > int64(math.MaxInt64/time.Second) {
		return 0, errors.Errorf("implausible granule position: %d samples", samples)
	}
	return time.Duration(seconds)*time.Second +
		time.Duration(remainder)*time.Second/sampleRate, nil
}

// opusPreSkip reads the encoder delay, in 48kHz samples, that a decoder
// discards and that therefore is not part of the playable length.
func opusPreSkip(head []byte) (uint16, error) {
	i := bytes.Index(head, []byte("OpusHead"))
	if i < 0 || len(head)-i < opusHeadSize {
		return 0, errors.New("not an Ogg/Opus file: no OpusHead")
	}
	return binary.LittleEndian.Uint16(head[i+10 : i+12]), nil
}

// lastGranule finds the granule position of the last page that completes a
// packet. A page carrying only a continued packet reports -1, so the scan
// keeps walking backwards past those.
//
// The version byte is checked because tail may begin mid-page, where the magic
// could appear inside a payload.
func lastGranule(tail []byte) (int64, error) {
	for i := len(tail) - oggPageHeaderSize; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+4], oggMagic) || tail[i+4] != 0 {
			continue
		}
		// Signed on purpose: -1 is the "no packet completes here" marker.
		granule := int64(binary.LittleEndian.Uint64(tail[i+6 : i+14]))
		if granule >= 0 {
			return granule, nil
		}
	}
	return 0, errors.New("no Ogg page with a granule position")
}
