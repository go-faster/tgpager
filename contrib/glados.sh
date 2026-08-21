#!/bin/sh
# GLaDOS-ish speech: espeak-ng says the words, ffmpeg supplies the panel they
# come out of. No model weights, no GPU, no network.
#
# Use it from the command provider:
#
#   tts:
#     provider:
#       type: command
#       name: ./contrib/glados.sh
#       args: ["{{text}}", "{{output}}"]
#       output_format: wav
#
# Needs espeak-ng and ffmpeg on PATH. With Nix:
#   nix shell nixpkgs#espeak-ng
set -eu

text=${1:?usage: glados.sh <text> <output>}
out=${2:?usage: glados.sh <text> <output>}

# Flat, slightly slow, female variant: GLaDOS is bored of you.
VOICE=${GLADOS_VOICE:-en-us+f3}
RATE=${GLADOS_RATE:-150}
PITCH=${GLADOS_PITCH:-40}

# vibrato  the servo warble
# chorus   the doubled, metallic timbre
# aecho    a small hard-surfaced room
# band     a speaker behind a wall panel, not a studio microphone
FILTER=${GLADOS_FILTER:-vibrato=f=5:d=0.10,chorus=0.7:0.9:40|55:0.35|0.30:0.3|0.4:2|1.5,aecho=0.8:0.88:10:0.35,highpass=f=250,lowpass=f=5200,volume=2.5}

espeak-ng -v "$VOICE" -s "$RATE" -p "$PITCH" --stdout -- "$text" |
	ffmpeg -hide_banner -loglevel error -y -i pipe:0 -af "$FILTER" -ar 48000 -ac 1 "$out"
