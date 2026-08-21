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

```console
tgpager -app-id $APP_ID -app-hash $APP_HASH -login
tgpager -app-id $APP_ID -app-hash $APP_HASH -peer @oncall -audio alert.ogg -token secret
```

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
