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

Every setting is documented in [CONFIG.md](CONFIG.md) and may be given in YAML
or as an environment variable, which wins. [config.schema.json](config.schema.json)
gives editors completion and validation:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/go-faster/tgpager/main/config.schema.json
```
 Credentials are omitted from that
generated reference so their values can never be formatted into an error; they
are:

| Setting | Environment |
| --- | --- |
| `telegram.app_hash` | `TGPAGER_TELEGRAM_APP_HASH` |
| `webhook.token` | `TGPAGER_WEBHOOK_TOKEN` |

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

Synthesis happens before the call is placed, and its result is cached by
content so a resent alert is not re-synthesized. **If it fails for any reason
the page still happens**, playing `audio` alone.

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
