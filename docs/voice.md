# Voice messages

Status: design.

A call is ephemeral. If the callee sleeps through it, or the phone was face
down in another room, all three attempts fail and nothing is left behind: no
artifact in any chat, nothing to scroll back to at 07:00, nothing to forward to
whoever actually owns the service. The audio we synthesized is discarded.

This adds sending the page as a Telegram **voice message**, so an unanswered
call still leaves something durable.

## The gap it closes

Calls and voice messages fail in opposite directions, which is the whole reason
to have both:

| | Call | Voice message |
| --- | --- | --- |
| Wakes someone | yes, it rings through silent mode on most setups | no, it is a notification |
| Survives being missed | no, nothing is recorded | yes, it stays in the chat |
| Forwardable | no | yes |
| Works while another call is active | no | yes |

So a voice message is a poor *pager* and a good *record*. It complements the
call rather than replacing it — but replacing it is worth allowing too, for the
alerts that should not ring anybody at 03:00.

## Placement

The synthesized audio already exists by the time the call is placed, so the
voice message costs one ffmpeg run and one upload. Nothing new is synthesized.

```
webhook → queue → synthesize → Ring ─── answered ──→ stream → hangup
                       │         │
                       │         └── all attempts failed ──┐
                       │                                   ▼
                       └────────────────────────────→ render ogg → upload → send
```

`mode` decides when the right-hand branch runs:

| Mode | Behaviour |
| --- | --- |
| `off` | never (default; today's behaviour) |
| `fallback` | only when every call attempt failed |
| `always` | on every page, whether or not the call was answered |
| `only` | instead of calling |

`fallback` is the interesting one and the reason for the feature. `always`
suits a team that wants a chat record of every page. `only` exists because
"tell me, but do not ring me" is a real routing decision, and Alertmanager can
already send different severities to different webhooks.

Notably `only` is the one mode where the send failing means the page failed
outright. Everywhere else it is a best-effort addendum to a call that already
happened.

## What is in it

Speech alone, once — not the call's audio.

The call plays **tone → speech → tone → speech → …** because the tone wakes
someone and the repeat covers a callee who answers mid-sentence. Neither
applies to a chat message: the notification is the attention-getter, and replay
is a tap. Repeating three times just makes a 30-second voice note out of a
10-second sentence.

When speech is unavailable — TTS disabled, or synthesis failed and the call
fell back to the tone — the voice message carries the tone instead. It says
less, but "something fired and the pager could not say what" is still worth
recording, and it is consistent with never letting a TTS failure delete a page.

No caption. gotd's `Voice` builder deliberately takes no caption argument,
unlike its `Audio` sibling, and a text page is a separate feature with its own
design questions (formatting, entities, length limits).

## Rendering

Telegram voice messages must be Ogg/Opus. Everything needed is already in
`internal/audio`: the same concat filter that feeds the RTP muxer, with an Ogg
muxer on the end instead.

```
ffmpeg -i speech.mp3 -filter_complex "[0:a]concat=n=1:v=0:a=1[out]" \
  -map "[out]" -vn -c:a libopus -ar 48000 -ac 1 -f ogg out.ogg
```

This is `audio.Spec` again, rendered to a file rather than streamed, so
`concatFilter` is shared and a spec that plays correctly in a call renders
correctly to a file by construction. Output goes to a temporary file, deleted
after the send: it is derived from cached audio and not worth persisting.

### Duration

A voice message with no duration shows as `0:00` and looks broken. The duration
is not knowable up front — it is however long the concatenated inputs came out.

Rather than add an `ffprobe` dependency, read it from the file we just wrote.
An Ogg page header carries a granule position, and for Opus that is a sample
count at 48 kHz; the last page's granule minus the `pre-skip` from `OpusHead`
is the exact playable length. Scanning back from EOF for the final page is
about thirty lines.

Verified on a real render: last granule 480312, pre-skip 312, so 480000/48000 =
10.000s, against ffprobe's 10.0065s (which includes the pre-skip).

No waveform. Telegram shows a flat bar without one, and computing a real
waveform means decoding the Opus we just encoded to get 5-bit amplitude buckets
— a lot of machinery for a cosmetic detail. It can be added later without
changing anything else.

## Sending

gotd has the whole path:

```go
sender.To(peer).
    Upload(message.FromPath(oggPath)).
    Voice(ctx)
```

`Voice` sets `audio/ogg` and `DocumentAttributeAudio{Voice: true}`; duration
goes on the same attribute.

Two small pieces of plumbing:

- `tgcall.Client` holds a resolved `tg.InputUserClass` for calls, and sending
  needs a `tg.InputPeerClass`. A conversion helper covers `*tg.InputUser` and
  `*tg.InputUserSelf`.
- `message.NewSender` is built once alongside the calls client, on the same
  `tg.Client`, so it shares the connection, the peer cache and gotd's tracing.

**Privacy settings differ between calls and messages.** An account that accepts
calls from you may still restrict who can message it, and vice versa. This is
worth discovering at deploy time rather than during a page, so `-check` should
grow a send to the peer — but that means an actual message arriving, which is
too rude to do on every start. Left out for now; the first `fallback` send will
surface it, and the error from Telegram is explicit.

## Configuration

```yaml
voice:
  mode: fallback      # off | fallback | always | only
  timeout: 60s
  attempts: 3
```

A plain object, not an optional one: `mode: off` is a complete way to say
"disabled", so there is no reason for the section's presence to also mean
something. Defaults keep today's behaviour exactly.

`attempts` reuses `Client.retry`, which already exists for calls and is generic
over the attempt. An upload is a multi-request operation over a connection that
may be re-establishing, so a single try is optimistic.

## Failure policy

Same principle as TTS: **a page must never fail because the extra channel
failed.**

| Failure | Behaviour |
| --- | --- |
| render fails | log, count, no voice message; the call already happened |
| upload or send fails | log, count, retried, then given up on |
| peer rejects messages | logged with Telegram's error, same as above |
| any of the above in `only` mode | the page failed; logged at error |

The ordering matters: in `fallback` mode the send happens after `Ring` has
already exhausted its attempts, so it can never delay or consume a call
attempt.

## Observability

Span `tgcall.SendVoice` with mode, duration and byte count. Counter
`tgpager.voice.messages{result=sent|error, mode=…}`.

The spoken text is not on the span, for the same reason it is not on the TTS
spans.

## Testing

Rendering is testable without Telegram: render a known spec, assert the file is
Ogg/Opus and that the parsed duration matches ffprobe, and skip when ffmpeg is
absent the way the existing audio tests do. The granule parser gets a table
test plus a fuzzer over truncated and garbage files — it reads offsets out of
a file, which is exactly the shape that panics on a short read.

The send path is tested against a mock `tg.Invoker`, asserting the request
carries `Voice: true`, the right MIME and a non-zero duration.

The test that matters most: `mode: fallback` with a call that never connects
still produces a voice message, and a send that fails does not turn a
successful call into a failed page.

## Non-goals

- Text messages. Genuinely useful, genuinely a separate feature.
- Receiving anything: acknowledgements, `/ack` commands, escalation on silence.
  That turns the pager into a bot with state, which is a different program.
- Waveforms.
- Group chats. The peer is one user today; broadening it is orthogonal.
