#!/usr/bin/env bash
# Runs a command with PORTO_TEST_POSTGRES_URL pointing at a PostgreSQL database.
#
#   scripts/with-postgres.sh go test -tags=integration ./...
#
# - If PORTO_TEST_POSTGRES_URL is already set, it is used as is.
# - Otherwise an ephemeral container ($POSTGRES_IMAGE, default postgres:17-alpine) is started on a random
#   local port and removed on exit.
# - Without docker the command runs on SQLite only, with a warning.
set -euo pipefail

if [ "$#" -eq 0 ]; then
	echo "usage: $0 <command> [args...]" >&2
	exit 2
fi

if [ -n "${PORTO_TEST_POSTGRES_URL:-}" ]; then
	exec "$@"
fi

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "WARNING: docker not available: running on SQLite only (PostgreSQL tests are skipped)" >&2
	exec "$@"
fi

image="${POSTGRES_IMAGE:-postgres:17-alpine}"
name="porto-test-pg-$$-$RANDOM"
password="porto-test"

cleanup() { docker rm -f -v "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

# The data directory lives in memory (tmpfs): no anonymous volume is left behind and the run does not
# depend on free disk space in the docker VM.
docker run -d --rm --name "$name" --shm-size=1g \
	--tmpfs /var/lib/postgresql/data:rw,size=3g --tmpfs /var/run/postgresql:rw \
	-e POSTGRES_USER=porto -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB=porto_test \
	-p 127.0.0.1::5432 "$image" \
	-c fsync=off -c synchronous_commit=off -c full_page_writes=off -c max_connections=300 -c max_locks_per_transaction=512 >/dev/null

port="$(docker port "$name" 5432/tcp | head -n1 | sed 's/.*://')"
if [ -z "$port" ]; then
	echo "with-postgres: could not read the container port" >&2
	exit 1
fi

for _ in $(seq 1 60); do
	if docker exec "$name" pg_isready -U porto -d porto_test >/dev/null 2>&1 &&
		docker exec "$name" psql -U porto -d porto_test -c 'select 1' >/dev/null 2>&1; then
		break
	fi
	sleep 0.5
done
if ! docker exec "$name" pg_isready -U porto -d porto_test >/dev/null 2>&1; then
	echo "with-postgres: PostgreSQL did not become ready" >&2
	docker logs "$name" >&2 || true
	exit 1
fi

export PORTO_TEST_POSTGRES_URL="postgres://porto:${password}@127.0.0.1:${port}/porto_test?sslmode=disable"
echo "with-postgres: using ${image} on 127.0.0.1:${port}" >&2
"$@"
