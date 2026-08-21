# tgpager configuration

## Configuration

| Name | Type | Required | Default | Constraints | yaml | env | Description |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [`telegram`](#telegram) | object | yes |  |  | `telegram` |  |  |
| [`webhook`](#webhook) | object | yes |  |  | `webhook` |  |  |
| [`call`](#call) | object | yes |  |  | `call` |  |  |
| `peer` | string | yes |  | non-empty | `peer` | `TGPAGER_PEER` | Call target: @username, phone, t.me link, or id:<user-id>[:<access-hash>]. |
| `audio` | string | yes |  | non-empty | `audio` | `TGPAGER_AUDIO` | Audio file played into the call. |
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
