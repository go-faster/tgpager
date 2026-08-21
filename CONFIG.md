# tgpager configuration

## Configuration

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [`telegram`](#telegram) | object | yes |  |  | `telegram` |  |  |
| [`webhook`](#webhook) | object | yes |  |  | `webhook` |  |  |
| [`call`](#call) | object | yes |  |  | `call` |  |  |
| [`tts`](#tts) | object | no |  |  | `tts` |  |  |
| `peer` | string | yes |  | non-empty | `peer` | `TGPAGER_PEER` | Call target: @username, phone, t.me link, or id:<user-id>[:<access-hash>]. |
| `audio` | string | yes |  | non-empty | `audio` | `TGPAGER_AUDIO` | Audio file played into the call, and the tone before speech. |
| `peer_cache` | string | no | `"peers.bolt"` | non-empty | `peer_cache` | `TGPAGER_PEER_CACHE` | Path to the peer access hash cache. Account-scoped. |
| `debug` | boolean | no | `false` |  | `debug` | `TGPAGER_DEBUG` | Enable debug logging. |

## telegram

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `app_id` | integer | yes |  | at least 1, at most 2147483647 | `telegram.app_id` | `TGPAGER_TELEGRAM_APP_ID` | Telegram application ID. |
| `session` | string | no | `"session.json"` | non-empty | `telegram.session` | `TGPAGER_TELEGRAM_SESSION` | Path to the session file. Holds credentials; keep it private. |

## webhook

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `addr` | string | no | `":8080"` | non-empty | `webhook.addr` | `TGPAGER_WEBHOOK_ADDR` | HTTP listen address. |
| `queue_size` | integer | no | `100` | at least 1, at most 10000 | `webhook.queue_size` | `TGPAGER_WEBHOOK_QUEUE_SIZE` | How many pages may wait to be placed. |

## call

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ring_timeout` | duration | no | `45s` | at least 1s | `call.ring_timeout` | `TGPAGER_CALL_RING_TIMEOUT` | How long an unanswered call keeps ringing. |
| `connect_timeout` | duration | no | `30s` | at least 1s | `call.connect_timeout` | `TGPAGER_CALL_CONNECT_TIMEOUT` | How long an accepted call may take to negotiate media. |
| `attempts` | integer | no | `3` | at least 1, at most 100 | `call.attempts` | `TGPAGER_CALL_ATTEMPTS` | How many times to place a call before giving up. |
| `retry_delay` | duration | no | `10s` | at least 0s | `call.retry_delay` | `TGPAGER_CALL_RETRY_DELAY` | Delay between call attempts. |

## tts

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [`provider`](#ttsprovider-type-openai) | union | yes |  |  | `tts.provider` |  | Speech provider. Omit to page with the audio file alone. |
| `template` | string | no |  |  | `tts.template` | `TGPAGER_TTS_TEMPLATE` | Go template rendered into the spoken sentence. |
| [`cache`](#ttscache) | object | yes |  |  | `tts.cache` |  |  |
| `repeat` | integer | no | `3` | at least 1, at most 10 | `tts.repeat` | `TGPAGER_TTS_REPEAT` | How many times to play tone and speech, so a groggy callee gets a second chance. |
| `timeout` | duration | no | `10s` | at least 1s | `tts.timeout` | `TGPAGER_TTS_TIMEOUT` | How long to wait for synthesis before paging without speech. |

## tts.provider (type: openai)

Speech provider. Omit to page with the audio file alone.

Selected by `type: openai`.

| Name | Type | Required | Default | Values | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `type` | string | yes |  | `openai` |  | `tts.provider.type` | `TGPAGER_TTS_PROVIDER_TYPE` | Selects this variant. |
| `base_url` | string | no | `"https://api.openai.com/v1"` |  | non-empty | `tts.provider.base_url` | `TGPAGER_TTS_PROVIDER_BASE_URL` | Base URL of the speech endpoint, without a trailing /audio/speech. |
| `model` | string | yes |  |  | non-empty | `tts.provider.model` | `TGPAGER_TTS_PROVIDER_MODEL` | Speech model, for example openai/gpt-4o-mini-tts. |
| `voice` | string | no | `"alloy"` |  |  | `tts.provider.voice` | `TGPAGER_TTS_PROVIDER_VOICE` | Voice name, as understood by the model. |
| `format` | string | no | `"mp3"` |  | non-empty | `tts.provider.format` | `TGPAGER_TTS_PROVIDER_FORMAT` | Audio format to request. |
| `instructions` | string | no |  |  |  | `tts.provider.instructions` | `TGPAGER_TTS_PROVIDER_INSTRUCTIONS` | How to deliver the line, for example "Speak urgently and clearly". Ignored by older models such as tts-1. |
| `speed` | number | no |  |  | at least 0.25, at most 4 | `tts.provider.speed` | `TGPAGER_TTS_PROVIDER_SPEED` | Playback multiplier. Unset leaves it to the provider. |
| `dialect` | string | no | `"openai"` | `"openai"`, `"openrouter"` |  | `tts.provider.dialect` | `TGPAGER_TTS_PROVIDER_DIALECT` | Where instructions go on the wire: top level for openai, nested for openrouter. |

## tts.provider (type: command)

Speech provider. Omit to page with the audio file alone.

Selected by `type: command`.

| Name | Type | Required | Default | Values | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `type` | string | yes |  | `command` |  | `tts.provider.type` | `TGPAGER_TTS_PROVIDER_TYPE` | Selects this variant. |
| `name` | string | yes |  |  | non-empty | `tts.provider.name` | `TGPAGER_TTS_PROVIDER_NAME` | Executable to run, for example piper. |
| `args` | list of string | no |  |  |  | `tts.provider.args` | `TGPAGER_TTS_PROVIDER_ARGS` | Arguments. {{text}} is replaced by the text to speak, otherwise it is written to stdin; {{output}} is replaced by a temporary file to write, otherwise audio is read from stdout. |
| `output_format` | string | no | `"wav"` |  | non-empty | `tts.provider.output_format` | `TGPAGER_TTS_PROVIDER_OUTPUT_FORMAT` | Audio format the command produces. |

## tts.cache

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `dir` | string | no | `"tts-cache"` | non-empty | `tts.cache.dir` | `TGPAGER_TTS_CACHE_DIR` | Directory holding synthesized audio, reused across resends. |
| `ttl` | duration | no | `720h0m0s` | at least 0s | `tts.cache.ttl` | `TGPAGER_TTS_CACHE_TTL` | How long unused audio is kept. Zero keeps it forever. |
| `max_bytes` | integer | no | `268435456` | at least 0 | `tts.cache.max_bytes` | `TGPAGER_TTS_CACHE_MAX_BYTES` | Size the cache is trimmed to, dropping least recently used audio. Zero is unbounded. |
