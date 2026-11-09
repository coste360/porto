#!/usr/bin/env bash
# Nightly reset of the Porto public demo (deploy/demo/README.md): stops the server, replaces every piece of data
# with a fresh demo marina, starts the server again and waits until it is ready. Run it on the Docker host, from
# cron or a systemd timer; it never touches other containers or volumes.
#
# env: COMPOSE_FILE_DEMO   compose file (default: the docker-compose.yml next to this script)
#      DEMO_RESET_CMD      porto command that wipes the data and loads the demo marina (default "seed demo --reset",
#                          run as "/porto seed demo --reset" with PORTO_DEMO_INSTANCE=true; it reads the demo
#                          password from PORTO_ADMIN_PASSWORD, docs/demo.md)
#      PULL=1              pull the image first (follow PORTO_VERSION, e.g. "latest")
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
compose_file="${COMPOSE_FILE_DEMO:-$here/docker-compose.yml}"
read -r -a reset_cmd <<<"${DEMO_RESET_CMD:-seed demo --reset}"
dc() { docker compose -f "$compose_file" "$@"; }
log() { printf '%s porto-demo: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

if [ "${PULL:-0}" = 1 ]; then
	log "pulling the image"
	dc pull --quiet porto
fi
log "stopping the server"
dc stop porto
log "resetting the data (${reset_cmd[*]})"
if ! dc run --rm --no-deps -e PORTO_DEMO_INSTANCE=true porto "${reset_cmd[@]}"; then
	log "reset failed: starting the server again; if the guard refused, the previous data is intact, otherwise" \
		"the data may already be wiped (empty or partial demo) until the next reset loads the demo again"
	dc up -d porto
	exit 1
fi
log "starting the server"
dc up -d porto
port="$(dc port porto 8080 | sed 's/.*://')"
for _ in $(seq 1 60); do
	if curl -fsS "http://127.0.0.1:${port}/readyz" >/dev/null 2>&1; then
		log "ready"
		exit 0
	fi
	sleep 2
done
log "the server did not become ready within 2 minutes"
dc logs --tail 50 porto
exit 1
