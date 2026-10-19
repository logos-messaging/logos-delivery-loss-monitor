#!/bin/sh
# Creates deploy/secrets/ with a probe key and a listener token. Existing files are kept.
set -eu
cd "$(dirname "$0")"

umask 077
mkdir -p secrets
chmod 700 secrets

if [ ! -f secrets/probe_key ]; then
	openssl rand -hex 32 > secrets/probe_key
fi
if [ ! -f secrets/listener_token ]; then
	openssl rand -hex 32 | tr -d '\n' > secrets/listener_token
fi
if [ ! -f secrets/tokens ]; then
	printf 'listener report %s\n' "$(cat secrets/listener_token)" > secrets/tokens
fi

# The containers run as non-root users and read these through bind mounts.
chmod 644 secrets/probe_key secrets/listener_token secrets/tokens
echo "secrets ready in $(pwd)/secrets"
