package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// oggPage builds a page carrying payload, which is all OggDuration reads.
func oggPage(granule int64, payload []byte) []byte {
	segments := len(payload)/255 + 1
	page := make([]byte, 0, oggPageHeaderSize+segments+len(payload))
	page = append(page, oggMagic...)
	page = append(page, 0, 0)
	page = binary.LittleEndian.AppendUint64(page, uint64(granule))
	page = binary.LittleEndian.AppendUint32(page, 1)
	page = binary.LittleEndian.AppendUint32(page, 0)
	page = binary.LittleEndian.AppendUint32(page, 0)

	page = append(page, byte(segments))
	remaining := len(payload)
	for range segments {
		page = append(page, byte(min(remaining, 255)))
		remaining -= min(remaining, 255)
	}
	return append(page, payload...)
}

func opusHead(preSkip uint16) []byte {
	head := append([]byte("OpusHead"), 1, 1)
	head = binary.LittleEndian.AppendUint16(head, preSkip)
	head = binary.LittleEndian.AppendUint32(head, sampleRate)
	head = binary.LittleEndian.AppendUint16(head, 0)
	return append(head, 0)
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.ogg")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestOggDuration(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    time.Duration
		wantErr bool
	}{
		{
			name: "granule minus pre-skip",
			data: append(oggPage(0, opusHead(312)), oggPage(480312, []byte("audio"))...),
			want: 10 * time.Second,
		},
		{
			name: "zero pre-skip",
			data: append(oggPage(0, opusHead(0)), oggPage(sampleRate, []byte("audio"))...),
			want: time.Second,
		},
		{
			// A page holding only a continued packet reports -1, and the scan
			// must walk past it rather than read it as a length.
			name: "skips continued page",
			data: func() []byte {
				d := append(oggPage(0, opusHead(0)), oggPage(sampleRate, []byte("audio"))...)
				return append(d, oggPage(-1, []byte("more"))...)
			}(),
			want: time.Second,
		},
		{
			name: "granule below pre-skip is empty, not negative",
			data: append(oggPage(0, opusHead(312)), oggPage(100, []byte("audio"))...),
			want: 0,
		},
		{
			name:    "not opus",
			data:    oggPage(48000, []byte("VorbisHead")),
			wantErr: true,
		},
		{
			name:    "no pages at all",
			data:    opusHead(312),
			wantErr: true,
		},
		{
			name:    "empty",
			data:    nil,
			wantErr: true,
		},
		{
			// A file cut off mid-write still has complete pages behind the
			// stub, and the last of those is the answer.
			name: "ignores a truncated trailing header",
			data: func() []byte {
				d := append(oggPage(0, opusHead(0)), oggPage(sampleRate, []byte("audio"))...)
				return append(d, oggMagic...)
			}(),
			want: time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OggDuration(writeTemp(t, tt.data))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOggDurationMissingFile(t *testing.T) {
	_, err := OggDuration(filepath.Join(t.TempDir(), "nope.ogg"))
	require.Error(t, err)
}

// FuzzOggDuration guards the offset arithmetic. The parser reads lengths out
// of a file, which is the shape that panics on a short read.
func FuzzOggDuration(f *testing.F) {
	f.Add(append(oggPage(0, opusHead(312)), oggPage(480312, []byte("audio"))...))
	f.Add(oggPage(-1, opusHead(0)))
	f.Add([]byte("OggS"))
	f.Add([]byte("OpusHead"))
	f.Add([]byte{})

	path := filepath.Join(f.TempDir(), "fuzz.ogg")
	f.Fuzz(func(t *testing.T, data []byte) {
		require.NoError(t, os.WriteFile(path, data, 0o600))
		dur, err := OggDuration(path)
		if err == nil {
			require.GreaterOrEqual(t, dur, time.Duration(0))
		}
	})
}
