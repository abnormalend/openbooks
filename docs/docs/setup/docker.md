[![Docker Pulls](https://img.shields.io/docker/pulls/evanbuss/openbooks.svg)](https://hub.docker.com/r/
evanbuss/openbooks/)

The OpenBooks docker image allows you to run [Server Mode](../modes/server.md). A multi-platform Docker container is published to Docker Hub for each release.

## Docker Compose

For advanced configuration, I recommend using Docker Compose to keep track of container setup.

```yaml title="docker-compose.yml"
version: "3.3"
services:
  openbooks:
    container_name: OpenBooks
    image: evanbuss/openbooks:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - "~/Downoads/openbooks:/books"
    command: --persist --name
    environment:
      - BASE_PATH=/openbooks/
```

## Configuration

See the [configuration docs](../configuration.md) for a complete list of Server mode configuration options. Pass the configuration flags into the `command` property.

Use the `environment` property to optionally set a custom base path for the server.

## Running as a non-root user

The image is built on `gcr.io/distroless/static`, which has no shell and therefore **does not honor the linuxserver.io-style `PUID`/`PGID` environment variables** — there's no entrypoint script to read them. Setting `PUID=1000` will be silently ignored, and the container will continue to run as root.

Use Docker's native `--user` flag (or the compose `user:` directive) instead. The default port is `8080` (non-privileged), so this works without any command override.

### Docker CLI

```bash
# One-time on the host: make sure the bind mount is owned by your UID,
# otherwise the container can't write downloads to it.
sudo chown -R "$(id -u):$(id -g)" ~/Downloads/openbooks

docker run \
  --user "$(id -u):$(id -g)" \
  -p 8080:8080 \
  -v ~/Downloads/openbooks:/books \
  evanbuss/openbooks --name my_irc_name --persist
```

### Docker Compose

```yaml title="docker-compose.yml"
version: "3.3"
services:
  openbooks:
    container_name: OpenBooks
    image: evanbuss/openbooks:latest
    user: "${PUID}:${PGID}"  # native Docker user mapping; despite the
                             # PUID/PGID names this is NOT the
                             # linuxserver.io convention - we just
                             # reuse the env-var spelling for muscle
                             # memory.
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - "~/Downloads/openbooks:/books"
    command: --persist --name my_irc_name
    environment:
      - BASE_PATH=/openbooks/
```

Define `PUID` and `PGID` either in a `.env` file next to the compose file or as Portainer "Environment variables" on the stack:

```
PUID=1000
PGID=1000
```

After the container starts, verify the mapping took effect by checking host-side file ownership of any newly downloaded book — it should match your UID, not `0`.

## Image Tags

`evanbuss/openbooks:latest`

: The majority of users will want this image and will always be up to date with the latest release. Note that auto-updating between version could break configuration.[^1]

`evanbuss/openbooks:X.X.X`

: Version specific tags. Each time a new release is cut, a new version tagged image is published.

`evanbuss/openbooks:edge`

: Built from the latest development build. This image is best if you want to test the latest changes but be warned that it could be unstable and not work at all.

## Image Platforms

- `linux/amd64`
- `linux/arm64`
- `linux/arm`

[^1]: I personally auto-update all of my docker containers and haven't experienced many issues. Tools like [Watchtower](https://containrrr.dev/watchtower/) can check for updates, pull images, and restart containers automatically.
