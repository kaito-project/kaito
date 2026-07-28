#!/bin/sh
# export_sas_token_for_streaming.sh — transparent entrypoint wrapper for SAS-authenticated blob streaming.
#
# The SAS-fetch init container mints a short-lived SAS token and writes it as an env file
# (KEY=value lines) to STREAM_ENV_FILE. This wrapper sources that file so the inference
# process inherits AZURE_STORAGE_SAS_TOKEN, then exec's the original command unchanged.
#
# This keeps the token hand-off out of the main container's command string. STREAM_ENV_FILE 
# defaults to /mnt/streaming/env.
set -eu

STREAM_ENV_FILE="${STREAM_ENV_FILE:-/mnt/streaming/env}"

if [ -f "${STREAM_ENV_FILE}" ]; then
    set -a
    # shellcheck disable=SC1090
    . "${STREAM_ENV_FILE}"
    set +a
fi

exec "$@"
