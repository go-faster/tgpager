# tgpager

Alertmanager webhook that pages on-call by placing a **Telegram call** and
playing an audio file into it. A ringing phone is harder to sleep through than
a push notification.

```console
go install github.com/go-faster/tgpager/cmd/tgpager@latest
```

## Usage

Calls are placed from a Telegram **user** account, so a session is required
once up front. Bots cannot place calls.

```yaml
# tgpager.yml
telegram:
  app_id: 123456
  app_hash: "..."      # or TGPAGER_TELEGRAM_APP_HASH
peer: "@oncall"
audio: alert.ogg
webhook:
  token: "secret"      # or TGPAGER_WEBHOOK_TOKEN
```

```console
tgpager -login                    # once, interactive
tgpager -config tgpager.yml
```

[tgpager.example.yml](tgpager.example.yml) is a complete annotated config,
kept loadable by a test. Every setting is documented in [CONFIG.md](CONFIG.md) and may be given in YAML
or as an environment variable, which wins. [config.schema.json](config.schema.json)
gives editors completion and validation:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/go-faster/tgpager/main/config.schema.json
```

### Credentials

`telegram.app_hash`, `webhook.token` and `tts.provider.api_key` each accept
three spellings:

```yaml
app_hash: "0123456789abcdef"                   # literal
app_hash: {env: TGPAGER_TELEGRAM_APP_HASH}     # environment variable
app_hash: {file: /run/secrets/telegram_hash}   # file, relative to the config
```

The file spelling is the one an environment override cannot cover: Kubernetes,
Docker and systemd's `LoadCredential` all hand a process a path rather than a
variable. A trailing newline is stripped, so a secret written with `echo` reads
back as written. Setting more than one spelling is an error rather than a
silent precedence rule.

`TGPAGER_TELEGRAM_APP_HASH` and friends still work as plain environment
overrides.

Point an Alertmanager receiver at it:

```yaml
receivers:
  - name: tgpager
    webhook_configs:
      - url: http://tgpager:8080/alertmanager
        http_config:
          authorization:
            credentials: secret
```

`ffmpeg` must be on `PATH`; it encodes the audio to Opus RTP.

## Speech

By default a page plays `audio` alone. Add a `tts` section and the call says
what fired: the tone wakes the callee, the speech tells them what broke, and
the pair repeats so answering mid-sentence still works.

```yaml
tts:
  provider:
    type: openai            # OpenAI, OpenRouter, Azure, or a local compatible server
    base_url: https://openrouter.ai/api/v1
    model: openai/gpt-4o-mini-tts
    voice: alloy
    dialect: openrouter               # where instructions go on the wire
    instructions: Speak urgently and clearly.
  repeat: 3
```

Or synthesize locally, which keeps working when the network is the thing that
broke:

```yaml
tts:
  provider:
    type: command
    name: piper
    args: ["--model", "en_US.onnx", "--output_file", "{{output}}"]
```

[contrib/glados.sh](contrib/glados.sh) is a worked example of the `command`
provider: espeak-ng says the words, ffmpeg supplies the panel they come out of.
No weights, no GPU, no network — and it sounds like being told your test
chamber is on fire.

```yaml
tts:
  provider:
    type: command
    name: ./contrib/glados.sh
    args: ["{{text}}", "{{output}}"]
    output_format: wav
```

Synthesis happens before the call is placed, and its result is cached by
content so a resent alert is not re-synthesized. **If it fails for any reason
the page still happens**, playing `audio` alone.

The provider is exercised once at startup, which warms a model server that
loads weights lazily. A failure there is a warning, never a refusal to start —
a pager that will not boot is worse than one that only plays a tone. To fail a
deploy instead, run `tgpager -check`, which exits non-zero if the speech path
is broken.

## Voice messages

A call is ephemeral: missed, it records nothing. tgpager can also leave the
page in the chat as a Telegram voice message, which survives being missed and
can be forwarded to whoever owns the service.

```yaml
voice:
  mode: fallback    # off | fallback | always | only
```

| Mode | Behaviour |
| --- | --- |
| `off` | never send one (default) |
| `fallback` | send only after every call attempt failed |
| `always` | send on every page, answered or not |
| `only` | send instead of calling, for alerts that must not ring anybody |

It carries the speech alone, once. The tone and the repeat exist to wake
someone and to catch a callee answering mid-sentence, neither of which applies
to a chat message. When speech is unavailable it carries the tone, because
"something fired" still beats nothing.

Sending never delays or consumes a call attempt, and outside `only` mode a
failure to send is a warning: the call already happened.

To try it without waiting for an alert or ringing anybody:

```console
$ tgpager -voice
```

That sends one test voice message to `peer` and exits, whatever `voice.mode`
says. Point `peer` at your own account and it lands in Saved Messages.

### As a bot

Calls require a user account, but a voice message does not: it is an ordinary
document with a flag set. Authenticating with a bot token needs no phone, no
code and no session bootstrap, which suits an unattended deployment.

```yaml
telegram:
  bot_token:
    env: TG_BOT_TOKEN
voice:
  mode: only
```

`mode: only` is the only mode a bot can serve, and any other is rejected at
startup rather than failing at each page. The target must have started the bot
for it to be allowed to message them.

## Docker

[deploy/](deploy) is a working stack: tgpager plus a
[Style-Bert-VITS2](https://github.com/litagin02/Style-Bert-VITS2) service
speaking the [Portal GLaDOS voice](https://huggingface.co/WarriorMama777/GLaDOS_TTS),
so a page is announced by the voice most likely to be believed about a
cascading failure. See [deploy/README.md](deploy/README.md) for the setup, the
model download and running it as a bot.

## Sending a test alert

The webhook speaks Alertmanager's payload format:

```console
$ curl -X POST http://localhost:8080/alertmanager \
    -H 'Content-Type: application/json' \
    -H 'Authorization: Bearer secret' \
    -d '{
      "version": "4",
      "groupKey": "manual-test",
      "status": "firing",
      "commonLabels": {"alertname": "NodeExporterDown", "severity": "critical"},
      "commonAnnotations": {"summary": "node exporter has been down for 5 minutes"},
      "alerts": [{"status": "firing", "labels": {"alertname": "NodeExporterDown"}}]
    }'
```

Drop the `Authorization` header if `webhook.token` is unset. The handler queues
the page and returns immediately, so `202 Accepted` means accepted, not
delivered — watch the logs for the outcome. A resolved alert is accepted and
ignored; only `status: firing` pages.

`groupKey` deduplicates while a page is in flight: a second request naming a
`groupKey` already queued is accepted and dropped, so Alertmanager retrying
does not page twice.

## Peer

`-peer` accepts a username, phone, deeplink or raw ID:

| Form | Example |
| --- | --- |
| username | `@durov`, `durov` |
| phone | `+13115552368` |
| deeplink | `t.me/durov` |
| id | `id:1234567` |
| id with access hash | `id:1234567:9876543210` |

Resolved access hashes are cached in `-peer-cache` so a peer stays callable
across restarts. The cache is account-scoped: drop it when changing session.

## Telemetry

Traces and metrics are exported via OpenTelemetry, including gotd's own MTProto
spans. See [go-faster/sdk/app](https://github.com/go-faster/sdk) for the
`OTEL_*` environment variables.

## License

[Apache License 2.0](LICENSE)
