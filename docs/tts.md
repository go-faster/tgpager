# TTS provider support

Status: implemented.

Two things changed during implementation, both recorded below: the shared
`timeout` moved up out of the provider variants, and `command`'s format is
named `output_format`.

Today every page plays the same `-audio` file. The alert is decoded and then
discarded, so the callee learns only that *something* fired. This adds speech
synthesis so the call says what broke.

## The constraint everything follows from

tgpager exists to wake someone at 03:00, and it is most needed exactly when
infrastructure is broken. Synthesis is an HTTP call to a third party.

**A page must never fail because TTS failed.** Every decision below is
downstream of that sentence.

## Pipeline placement

Synthesis happens *before* the call is placed:

```
webhook → queue → synthesize → compose → Ring → connect → stream → hangup
                       │
                       └── error/timeout → static file
```

Not after connect: the callee would hear dead air for the length of an HTTP
round trip. Placing it before the ring costs 0.5–3s that nobody perceives,
because `calls.Request` already blocks while the phone rings.

## Interface

```go
// internal/tts
type Audio struct {
	Data   []byte
	Format string // "mp3", "pcm", "wav"
}

type Synthesizer interface {
	Synthesize(ctx context.Context, text string) (Audio, error)
}
```

`Format` is deliberately loose. ffmpeg already normalises everything to Opus
RTP, so no provider has to agree on a codec and none of them need converting
in Go.

## Providers

| Type | Covers | Why |
| --- | --- | --- |
| `none` | today's behaviour | static file only |
| `openai` | OpenAI, OpenRouter, Azure, local compatible servers | one client, `base_url` picks the vendor |
| `command` | piper, espeak-ng, say | offline, no API key, hermetic tests |

OpenRouter's TTS endpoint is OpenAI-compatible — `POST /api/v1/audio/speech`
with `{model, input, voice, response_format}` returning raw audio — so a single
HTTP client covers every hosted option through `base_url`. There is no reason
to write per-vendor code.

`command` carries weight out of proportion to its size: when the network is
down, the hosted provider is unreachable precisely when a page matters most. A
local binary always answers.

## Composition: one ffmpeg invocation, not three

The callee hears **tone → speech → tone → speech …**. The tone wakes them, the
speech tells them what broke, and the repeat covers answering mid-sentence.

The obvious implementation — call `Stream` once per segment — is wrong. Each
invocation starts a fresh ffmpeg with a fresh RTP muxer, which restarts
sequence numbers and timestamps. A receiver seeing sequence go backwards mid
call is entitled to drop the stream. **RTP continuity requires one ffmpeg
process for the whole call.**

Composition therefore happens inside ffmpeg, via the concat *filter* (not the
concat demuxer, which requires matching codecs and would force a normalisation
pass):

```
ffmpeg -re -i tone.ogg -i speech.mp3 \
  -filter_complex "[0:a][1:a][0:a][1:a]concat=n=4:v=0:a=1[out]" \
  -map "[out]" -c:a libopus -ar 48000 -ac 1 -f rtp rtp://…
```

Verified to work across differing codecs and sample rates (Ogg/Opus 48kHz plus
MP3 24kHz) in a single continuous stream.

This changes `audio.Streamer`, which currently takes one `file string`. It
needs to take an ordered segment list plus a repeat count.

## Configuration

```yaml
tts:
  provider:
    type: openai
    base_url: https://openrouter.ai/api/v1
    api_key: ...              # figureout.Secret()
    model: openai/gpt-4o-mini-tts
    voice: alloy
  template: |
    {{ .Status }} alert. {{ .CommonLabels.alertname }}.
    Severity {{ .CommonLabels.severity }}. {{ .CommonAnnotations.summary }}
  repeat: 3
  timeout: 10s
```

A figureout `OneOf` with `Discriminator("type")` and one `Variant` per provider
gives per-variant validation, documentation and JSON schema for free.

The whole `tts` section is an optional object rather than an optional union,
because a figureout union is always required. Absent means speech is off.

**Variants share their path.** `tts.provider.format` is one path whichever
variant is selected, so a field name repeated across variants becomes a single
path with two environment aliases: setting the one named for `command` would
silently configure `openai`. Verified by experiment, not assumed. Hence the
shared `timeout` lives on `tts` where it is genuinely shared, and `command`
names its format `output_format` — a format the binary *produces*, which is a
different thing from a format `openai` is *asked* for.

Rendered text is capped in length. Alert labels come from metrics and are
attacker-influenceable, and an unbounded label should not turn into an
unbounded bill.

## Delivery

`instructions` steers how the line is read — "Speak urgently and clearly" — on
models that support it. The wire shape differs: OpenAI takes it at the top
level, OpenRouter nests it under `provider.options.openai`. A `dialect` field
says which, rather than the code guessing from the URL. `speed` is optional,
because zero is a real multiplier and not a way to say "unset".

Both feed the cache fingerprint along with model and voice: changing how a line
is delivered must not serve the previous recording.

## Caching

Alertmanager resends a firing alert every `repeat_interval`, so an uncached
provider is re-billed for identical text forever.

Content-addressed on disk: `sha256(provider|model|voice|text)` →
`cache/<key>.<format>`, bounded by an LRU count. Files rather than bbolt,
because ffmpeg wants a path and the audio never needs to be in memory.

## Plumbing change

`server.CallRequest` carries only `GroupKey`; the decoded payload is dropped in
the handler. Templating needs the payload, so `CallRequest` grows a field. It
is a small change that touches the webhook-to-queue boundary.

## Failure policy

| Failure | Behaviour |
| --- | --- |
| provider error or timeout | log, count, fall back to the static file |
| template error | fall back |
| cache read error | re-synthesise |
| static file missing | already rejected at startup |

Falling back is never silent: it increments `tgpager.tts.fallbacks` and is
recorded on the span. A page always happens.

Notably, TTS failure does **not** consume a call attempt. Retries exist for
unanswered calls; burning them on a hard-down provider could mean never paging
at all.

## Observability

Span `tts.Synthesize` with provider, model, cache hit and byte count. Counters
`tgpager.tts.requests{result=hit|miss|error}` and `tgpager.tts.fallbacks`, plus
a synthesis duration histogram.

The rendered text is never attached to a span or log. Alert labels can carry
hostnames, customer identifiers and worse.

## Testing

A fake `Synthesizer` drives the pipeline. The OpenAI client is tested against
`httptest`, including a non-2xx body and a hanging server for the timeout path.
The `command` provider runs a real local binary and skips when absent, the way
the ffmpeg tests already do. Templates get golden files.

The test that matters most: a provider that fails, and a provider that hangs
past its timeout, both still place a call and still play audio.
