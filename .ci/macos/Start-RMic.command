#!/bin/bash
# Run in Terminal so the configuration and local pairing approval stay visible.
cd "$(dirname "$0")" || exit 1
./rmic --setup "$@"
result=$?
if [[ $result != 0 ]]; then
  printf '\nRMic exited with code %s. Press Return to close.\n' "$result"
  read -r _
fi
exit "$result"
