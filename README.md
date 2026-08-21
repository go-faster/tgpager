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

## Docker

[deploy/docker-compose.yml](deploy/docker-compose.yml) and
[deploy/tgpager.yml](deploy/tgpager.yml) are a working stack. From `deploy/`:

```console
$ mkdir -p secrets
$ printf '%s' "$APP_HASH"     > secrets/telegram_app_hash
$ printf '%s' "$WEBHOOK_TOKEN" > secrets/webhook_token
$ cp /path/to/alert.ogg .
$ $EDITOR tgpager.yml            # app_id and peer
```

Then log in once. It is interactive — Telegram sends a code — and it is the
one step that cannot be automated:

```console
$ docker compose run --rm -it tgpager -login
$ docker compose up -d
```

The session lands on a named volume, so restarts and image upgrades do not
require logging in again. Check it before trusting it:

```console
$ docker compose run --rm tgpager -check    # config and speech provider
$ docker compose run --rm tgpager -voice    # sends one test voice message
```

Three things that are easy to get wrong, and are already handled here:

- **Secrets are files.** Docker mounts them under `/run/secrets`, so the config
  uses the `{file: …}` spelling rather than baking credentials into an image or
  a compose file.
- **Metrics bind to localhost** by default, where nothing outside the container
  can reach them. The compose file sets `METRICS_ADDR` to fix that, and exposes
  `/metrics` only on the host loopback.
- **The container runs unprivileged**, so the data directory is created in the
  image and owned by that user. A named volume mounted on a path the image does
  not have is created root-owned, and login then fails with `Permission denied`.

There is no health endpoint; the healthcheck uses `/metrics`, which is enough
to tell a live process from a wedged one.

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
