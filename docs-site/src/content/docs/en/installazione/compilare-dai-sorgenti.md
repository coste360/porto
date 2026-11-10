---
title: Building from source
description: Build the porto binary and the Docker image from the repository, and run it without Docker.
sidebar:
  order: 4
---

**Prerequisites:** Go 1.27 or later, Node.js 22 or later with npm, `make` and `git`. Alternatively open the
repository in the included *devcontainer*.

```bash
git clone https://github.com/coste360/porto.git && cd porto
make setup     # Go modules, golangci-lint in ./bin, web and e2e dependencies
make build     # builds the two web apps and bin/porto (static, no CGO)
bin/porto version
```

`make build` embeds the panel and the public page in the binary: `bin/porto` is all you need to run Porto.

## Running without Docker

```bash
export PORTO_BASE_URL=https://porto.my-marina.example
export PORTO_SECRET_KEY="$(openssl rand -base64 32)"   # keep it with your backups
export PORTO_DATA_DIR=/var/lib/porto
bin/porto serve
```

The server listens on `:8080` (`PORTO_ADDR`) and applies migrations on start (`PORTO_AUTO_MIGRATE=true`). The other
variables are in [Configuration](/en/installazione/configurazione/). Keep it running with the system service manager
(for example a systemd unit with `Restart=on-failure` and the variables in an `EnvironmentFile` readable only by the
service user).

## Docker image

```bash
make docker      # image ghcr.io/coste360/porto:dev from deploy/Dockerfile
```

## Binary commands

```text
porto serve                     # start the server
porto migrate up|status [--check]
                                # apply or show migrations (--check: exit 1 when some are pending)
porto admin create --email E --name N --org ORG [--marina NAME --slug SLUG --timezone TZ --locale it|en]
                                # create the owner (password from PORTO_ADMIN_PASSWORD, --password-stdin or a prompt)
porto seed demo [--password-stdin]
                                # load the demo marina, with one user per role
porto backup --out FILE         # portable backup, the same for SQLite and PostgreSQL
porto restore --in FILE [--force]
                                # restore a backup (--force wipes the database first)
porto version                   # binary version
```

Exit codes: `0` success, `1` error, `2` usage error.

## Desktop app

The desktop launcher is built with `make desktop-build` (opens the panel in the browser) or
`make desktop-build DESKTOP_TAGS=webview` (own window, needs CGO and the system toolchain). Unsigned packages come
from `make desktop-package-macos`, `make desktop-package-windows` and `make desktop-package-linux`. Details in
[docs/desktop-build.md](https://github.com/coste360/porto/blob/main/docs/desktop-build.md).
