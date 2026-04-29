<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# setup

## Purpose
Install / deployment guides for the two distribution channels: prebuilt binaries from GitHub Releases and the Docker image (`evanbuss/openbooks` on Docker Hub).

## Key Files
| File | Description |
|------|-------------|
| `binary.md` | Steps for downloading a release binary, making it executable, and running it. |
| `docker.md` | `docker run` examples covering the basic case, persistence via `-v`, and `BASE_PATH` for reverse-proxy deployments. |

## For AI Agents

### Working In This Directory
- The `BASE_PATH` examples in `docker.md` must include both leading and trailing forward slashes (e.g. `/openbooks/`) — this is enforced by `cmd/openbooks/util.go:sanitizePath` on the server side.
- The Docker image's `EXPOSE 80` and entrypoint flags are defined in the root `Dockerfile`; keep `docker.md` examples in sync if those change.

<!-- MANUAL: -->
