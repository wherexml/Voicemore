#!/bin/sh
set -eu
echo 'The old fixture shared production identity/data and is retired. Use scripts/accept-update-identity.py for disposable, signed real-app upgrade acceptance.' >&2
exit 1
