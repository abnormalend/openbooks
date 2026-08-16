# Configuration

There are plans to allow configuration via environment variables and config files in a future release.
For now, all config options are supplied via command line arguments / flags.

## Global Options

These options apply to both Server and CLI mode.

| Flag             | Default                   | Description                                                          |
|------------------|---------------------------|----------------------------------------------------------------------|
| `--debug`        | `false`                   | Display additional debug information, including all config values.   |
| `--help`/ `-h`   |                           | Display all commands and flags.                                      |
| `--log`/`-l`     | `false`                   | Save raw IRC logs for each client connection.                        |
| `--name`/`-n`    | **REQUIRED**              | Username used to connect to IRC server.                              |
| `--searchbot`    | `search`                  | The IRC search operator to use. Try `searchook` if `search` is down. |
| `--server`/`-s`  | `irc.irchighway.net:6697` | The IRC `server:port` to connect to.                                 |
| `--tls`          | `true`                    | Connect to IRC server over TLS.                                      |
| `--useragent/-u` | `OpenBooks v4.5.0`        | UserAgent / Version Reported to IRC Server.                          |

## Server Mode Options

| Flag                      | Default     | Description                                               |
|---------------------------|-------------|-----------------------------------------------------------|
| `--api-idle-timeout`      | `5m`        | Disconnect the API's IRC session after this long with no jobs. |
| `--api-token`             | *(unset)*   | Bearer token for the REST API. Falls back to `$OPENBOOKS_API_TOKEN`. Unset disables the API. See [REST API](api.md). |
| `--basepath`              | `/`         | Web UI Path. Must have trailing `/`. (Ex. `/openbooks/`)  |
| `--browser`/`-b`          | `false`     | Open the browser on startup.                              |
| `--dir`/`-d`              | `/temp`[^1] | Directory where search results and eBooks are saved.      |
| `--download-job-timeout`  | `10m`       | Fail an API download job if the bot hasn't offered the file in time. |
| `--no-browser-downloads`  | `false`     | Don't send files to browser but save them to disk.        |
| `--persist`               | `false`     | Save eBook files after sending to browser.                |
| `--port`/`-p`             | `5228`      | The port that the server listens on.                      |
| `--rate-limit`/`-r`       | `10`        | Seconds to wait between IRC search requests. (minimum 10) |
| `--search-job-timeout`    | `2m`        | Fail an API search job if the bot hasn't answered in time. |

## CLI Mode Options

| Flag         | Default           | Description                                          |
|--------------|-------------------|------------------------------------------------------|
| `--dir`/`-d` | Working Directory | Directory where search results and eBooks are saved. |

[^1]: Docker sets a static directory of `/books` so that the volume is accessible outside the container.
