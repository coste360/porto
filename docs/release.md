# Release procedure

How a version of Porto is published. Releases are cut by the maintainers from `main`; nothing here needs a secret
beyond the repository's own `GITHUB_TOKEN`.

## What a release produces

| Output | Built by | Where |
|---|---|---|
| Tag `vX.Y.Z` | maintainer | `github.com/coste360/porto` |
| Container image (amd64 + arm64, SBOM and provenance) | `.github/workflows/release.yml`, job `image` | `ghcr.io/coste360/porto:X.Y.Z`, `X.Y`, `X` (from 1.0.0), `latest` (stable only) |
| GitHub Release (notes from `CHANGELOG.md`, `api/openapi.yaml` attached, no binaries) | `release.yml`, job `release` | Releases page |
| Signed desktop packages (macOS dmg, Windows installer, Linux AppImage) and server archives | private build pipeline `porto-build`, which polls the latest GitHub Release every 15 minutes | Coste360 bucket, served by `https://updates.coste360.it/porto/...` |
| `latest.json` (stable releases only) | `porto-build` | `https://updates.coste360.it/porto/latest.json`, read by the desktop update banner |

Binaries are never attached to the GitHub Release: the desktop packages need Coste360's signing certificates and
are built outside this repository. The private pipeline checks out the tag and runs this repository's own entry
points (`make web-build`, `make desktop-package-*`, the `make build` flags): what it ships is exactly the tagged
source.

## Versioning

- [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`. Before 1.0.0 a minor version may contain
  breaking changes; from 1.0.0 a breaking change of the REST API, of the configuration or of a migration path needs
  a new major version.
- Tags are `vX.Y.Z` (stable) or `vX.Y.Z-rc.N` (pre-release). `release.yml` refuses any other tag.
- A pre-release gets an image tag `X.Y.Z-rc.N` only (no `latest`, no `X.Y`), a GitHub pre-release, and does not
  replace `latest.json`: installed desktop apps never see it.
- The version stamped into the binaries comes from the tag (`scripts/version.sh`, `git describe`); there is no
  version file to edit. The desktop packages carry only `X.Y.Z` (`NUMERIC_VERSION` of `scripts/desktop/common.sh`).
- Database migrations only move forward and run at start: a release never edits a migration already on `main`.

## Steps

1. **Freeze.** The last run of `ci.yml` on `main` (push, nightly or manual: the full suite, not the quick
   pull request checks) is green, and `desktop.yml` too when desktop files changed. Locally:
   `make check-full`. Look at the last `perf.yml` run (Performance: wall-clock budgets and the M12 load test,
   ADR 0019): it does not block the release, but a budget over its limit is worth a measure before tagging.
2. **API version.** Set `info.version` in `api/openapi.yaml` to `X.Y.Z` and run `make gen` (API first: never edit
   the generated files). Commit with the changelog.
3. **Changelog.** In `CHANGELOG.md` rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, add a new empty
   `## [Unreleased]` above it, and check that the section lists user-visible changes, migrations, configuration
   changes and known gaps. `release.yml` publishes this section as the release notes and fails if it is missing or
   empty.
4. **Commit** `chore(release): vX.Y.Z` on `main` (through a pull request like any other change).
5. **Tag and push:**

   ```sh
   git switch main && git pull --ff-only
   git tag -a vX.Y.Z -m "Porto vX.Y.Z"
   git push origin vX.Y.Z
   ```

6. **Watch `Release`** in GitHub Actions: `verify` (tag format and changelog section), `image` (multi-arch build,
   push to GHCR, `porto version` smoke test), then `release` (GitHub Release marked *latest*, or *pre-release*).
7. **Desktop packages.** Within about 15 minutes `porto-build` starts its `Build` for the new tag. When it is green,
   check:

   ```sh
   curl -s https://updates.coste360.it/porto/latest.json | head -20   # version X.Y.Z
   curl -sI https://updates.coste360.it/porto/download/latest/macos     # 302 to the dmg
   ```

   A failed build is not retried by itself: fix the cause and start `Build` by hand with the tag.
8. **Announce**: release notes link, the feature status table of the docs site
   (`docs-site/src/content/docs/installazione/index.mdx` and its English twin) if a feature moved, and the public demo (deploy/demo)
   picks the new image at its next nightly reset when it follows `latest`.

## The image on GHCR

- The first publish creates the package `ghcr.io/coste360/porto` as private: make it **public** once in the
  organisation's package settings (*Package settings → Change visibility*) and link it to the repository. Later
  releases keep the visibility.
- The image runs as non-root on a read-only file system; deployments pin `X.Y` to receive patch releases or
  `X.Y.Z` to stay put (`PORTO_VERSION` in `deploy/`).

## Desktop update check

The desktop app reads `https://updates.coste360.it/porto/latest.json` once a day when the version check is on
(`internal/desktop/update.go`, `DefaultUpdateURL`), with the User-Agent `porto/<version> (desktop; <goos>/<goarch>)`
and nothing else about the installation. The manifest has the updates service format (`version`, `tag`,
`published_at`, `release_notes`, `checksums`, `files[]`) plus a top-level `url`, the page the banner opens; unknown
fields are ignored, the body is read up to 64 KiB, and links must be `https`. Nothing is downloaded or installed.

The updates service must list `porto` in its `PRODUCTS`; until the first stable release `/porto/latest.json`
answers 503, which the app treats like any other failure (no banner).

## Fixing a release

- Wrong notes: edit `CHANGELOG.md` on `main`, then edit the GitHub Release text (re-running `release` on the same
  tag also updates the notes and the attached contract).
- Broken image or binaries: never move or reuse a tag. Fix on `main` and publish `vX.Y.(Z+1)`.
- Security fixes follow [SECURITY.md](../SECURITY.md): prepare the fix privately, publish the patch release, then
  the advisory.
