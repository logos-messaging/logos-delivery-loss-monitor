#!/bin/sh
# Usage: init-secrets.sh                      create deploy/secrets/ (existing files are kept)
#        init-secrets.sh add-token NAME SCOPE print a new token once and store only its SHA-256
set -eu
cd "$(dirname "$0")"

umask 077
mkdir -p secrets secrets/server
chmod 700 secrets
chmod 755 secrets/server

sha256_of() {
	printf '%s' "$1" | openssl dgst -sha256 -r | cut -d' ' -f1
}

publish() {
	chmod 644 "$@"
}

if [ "${1:-}" = "add-token" ]; then
	name="${2:-}"
	scope="${3:-}"
	case "$name" in
	'' | *[!A-Za-z0-9._-]*) echo "NAME must match [A-Za-z0-9._-]+" >&2; exit 2 ;;
	esac
	case "$scope" in
	report | query) ;;
	*) echo "SCOPE must be report or query" >&2; exit 2 ;;
	esac
	if [ ! -f secrets/server/tokens ]; then
		echo "run ./init-secrets.sh first" >&2
		exit 1
	fi
	if awk -v n="$name" '$1 == n { found = 1 } END { exit !found }' secrets/server/tokens; then
		echo "token name $name already exists" >&2
		exit 1
	fi
	token="$(openssl rand -hex 32)"
	printf '%s %s sha256:%s\n' "$name" "$scope" "$(sha256_of "$token")" >> secrets/server/tokens
	publish secrets/server/tokens
	echo "token for $name ($scope), shown once:"
	echo "$token"
	echo "reload the server: docker compose kill -s HUP server"
	exit 0
fi

if [ -f secrets/tokens ] && [ ! -f secrets/server/tokens ]; then
	mv secrets/tokens secrets/server/tokens
fi
if [ -f secrets/probe_key ] && [ ! -f secrets/server/probe_key ]; then
	mv secrets/probe_key secrets/server/probe_key
fi

if [ ! -f secrets/server/probe_key ]; then
	openssl rand -hex 32 > secrets/server/probe_key
fi
if [ ! -f secrets/listener_token ]; then
	openssl rand -hex 32 | tr -d '\n' > secrets/listener_token
fi
if [ ! -f secrets/server/tokens ]; then
	printf 'listener report sha256:%s\n' "$(sha256_of "$(cat secrets/listener_token)")" > secrets/server/tokens
fi

# The containers run as non-root users and read these through bind mounts.
publish secrets/server/probe_key secrets/server/tokens secrets/listener_token
echo "secrets ready in $(pwd)/secrets"
