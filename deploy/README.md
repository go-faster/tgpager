# Deploying tgpager

[docker-compose.yml](docker-compose.yml) and [tgpager.yml](tgpager.yml) are a
working stack: tgpager plus a
[Style-Bert-VITS2](https://github.com/litagin02/Style-Bert-VITS2) service
speaking the [Portal GLaDOS voice](https://huggingface.co/WarriorMama777/GLaDOS_TTS),
so a page is announced by the voice most likely to be believed about a
cascading failure.

Everything below runs from this directory.

## The speech service

Upstream publishes an image, `litagin/style-bert-vits2`, which ships no CMD
because it is built to be driven; the compose file drives it and mounts the
config in [glados/](glados). Synthesis runs on CPU, which is enough: upstream
is explicit that a GPU is needed for training, not inference. For a GPU,
uncomment the `deploy:` block in the compose file and set `server.device` to
`cuda` in [glados/config.yml](glados/config.yml).

The first start downloads several GB of BERT models onto a volume, which is why
its healthcheck allows fifteen minutes to come up. `docker compose up` will sit
there looking idle for a while; that is the download.

Drop the `glados` service and the `tts` section of `tgpager.yml` to page with
the tone alone.

## The voice

Weights are not vendored: they are 190 MB and not ours to redistribute. Fetch
the three files Style-Bert-VITS2 expects:

```console
$ mkdir -p glados/model_assets/Portal_GLaDOS_v1
$ base=https://huggingface.co/WarriorMama777/GLaDOS_TTS/resolve/main/Models/Style-Bert_VITS2/Portal_GLaDOS_v1
$ for f in Portal_GLaDOS_v1_e782_s50000.safetensors config.json style_vectors.npy; do
    curl -fL "$base/$f" -o "glados/model_assets/Portal_GLaDOS_v1/$f"
  done
```

Note the underscore in `Style-Bert_VITS2` upstream, which the local directory
does not have. Any Style-Bert-VITS2 voice works: the directory name is what
`model_name` refers to in the `tts` command in `tgpager.yml`.

The model is distributed under the CreativeML Open RAIL-M license. The voice is
Ellen McLain's performance as GLaDOS in Valve's *Portal*, which is worth
knowing before pointing it at anything public.

## Setup

```console
$ mkdir -p secrets
$ printf '%s' "$APP_HASH"      > secrets/telegram_app_hash
$ printf '%s' "$WEBHOOK_TOKEN" > secrets/webhook_token
$ cp /path/to/alert.ogg .
$ $EDITOR tgpager.yml            # app_id and peer
```

Then log in once. It is interactive — Telegram sends a code — and it is the one
step that cannot be automated:

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

## As a bot

A bot needs no phone number and no `-login` step: the session is created on
first start. Uncomment `telegram_bot_token` in both places it appears in the
compose file and `bot_token` in `tgpager.yml`, then write the token:

```console
$ printf '%s' "$TG_BOT_TOKEN" > secrets/telegram_bot_token
```

Bots cannot place calls — Telegram reserves those for users — so this requires
`voice.mode: only`, and tgpager refuses to start otherwise. The peer must have
messaged the bot at least once, or Telegram will not deliver to them.

## Things that are easy to get wrong

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
