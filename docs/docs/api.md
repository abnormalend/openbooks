# REST API

OpenBooks exposes a small job-based REST API so an external agent (or an MCP
server wrapping it) can do what a person does in the web UI: **search, read
the results, download one of them**. It lives under `<basepath>api/` — for a
Docker deployment with `BASE_PATH=/openbooks/` that is
`http://host:port/openbooks/api/`.

The full contract is served by the running instance at `GET …/api/openapi.yaml`.

## Enable it

Start the server with a token; without one every protected route answers
`503 {"code":"api_disabled"}`.

```bash
openbooks server --name mynick --persist --dir /books --api-token "$(openssl rand -hex 24)"
# or
OPENBOOKS_API_TOKEN=... openbooks server --name mynick --persist --dir /books
```

Send it as `Authorization: Bearer <token>`.

## One IRC identity

The API uses the same IRC nick as the browser UI, so only one of them may be
online at a time:

* while a browser tab is connected to the UI, `POST /api/search` and
  `POST /api/download` return `409 browser_session_active`;
* while an API job is running or queued, opening the UI is refused with 409.
  Once the queue drains the API disconnects from IRC after `--api-idle-timeout`
  (default 5m) — or immediately when a browser asks to connect.

## Walkthrough

```bash
API=http://192.168.1.84:5228/openbooks/api
H="Authorization: Bearer $OPENBOOKS_API_TOKEN"

# 0. handshake
curl -s $API/health | jq

# 1. search (202 → jobId)
JOB=$(curl -s -H "$H" -X POST $API/search \
      -d '{"query":"fourth wing","limit":25}' | jq -r .jobId)

# 2. poll until status is complete|error
curl -s -H "$H" $API/search/$JOB | jq '.status, .results[0]'
# {
#   "server":"DV8","author":"Rebecca Yarros","title":"Fourth Wing",
#   "format":"epub","size":"1.2MB",
#   "full":"!DV8 Rebecca Yarros - Fourth Wing.epub"
# }

# 3. download: pass a result's `full` string verbatim
DL=$(curl -s -H "$H" -X POST $API/download \
     -d '{"book":"!DV8 Rebecca Yarros - Fourth Wing.epub"}' | jq -r .jobId)

# 4. poll; bytes/size show progress, path/fileName appear on complete
curl -s -H "$H" $API/download/$DL | jq '{status,bytes,size,fileName,error}'
```

Job statuses: `queued → running → complete | error | cancelled`. Finished
jobs stay readable for one hour. A search that finds nothing is `complete`
with `"results": []`.

## Cancelling jobs

`DELETE /api/search/{jobId}` and `DELETE /api/download/{jobId}` cancel a
queued or running job:

* **Queued** — the job is marked `cancelled` in place and skipped when its
  turn comes up; the queue slot is freed immediately.
* **Running** — the in-flight IRC wait or DCC transfer is aborted; the job
  is finalized as `cancelled` once the worker observes the cancellation
  (usually within a second).

A job that already reached `complete`, `error`, or `cancelled` can't be
cancelled again: `DELETE` on it returns `409 {"code":"not_cancellable"}`.
An unknown `jobId` returns `404 {"code":"job_not_found"}`, same as `GET`.

```bash
curl -s -H "$H" -X DELETE $API/download/$DL | jq '.status'
# "cancelled"
```

## Book string formats

The `book` string identifying a search result — the `full` field returned
by `GET /search/{jobId}` — varies by IRC server. Treat it as **opaque**:
don't parse it, just pass it back verbatim as `POST /download`'s `book`.
It always starts with `!`. Formats seen in the wild:

| Server family | Shape | Example |
|---|---|---|
| Most servers (bare) | `!Server Author - Title (format).ext` | `!DV8 Rebecca Yarros - Fourth Wing.epub` |
| Firebound | leading `%HASH%` token | `!Firebound %A1B2C3% Author - Title.epub` |
| TrainFiles | leading `hash \|` token | `!TrainFiles a1b2c3 \| Author - Title.epub` |

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/health` | no | version, uptime, `ircConnected`, `browserConnected`, queue counts |
| GET | `/openapi.yaml` | no | this API's OpenAPI 3 document |
| POST | `/search` | yes | `{"query","limit"}` → `202 {jobId,status,position}` |
| GET | `/search/{jobId}` | yes | search job + results |
| DELETE | `/search/{jobId}` | yes | cancel a queued or running search job → `200` job (`status: cancelled`) |
| POST | `/download` | yes | `{"book"}` → `202 {jobId,status,position}` |
| GET | `/download/{jobId}` | yes | download job + progress + final path |
| DELETE | `/download/{jobId}` | yes | cancel a queued or running download job → `200` job (`status: cancelled`) |
| GET | `/jobs?type=` | yes | all live jobs, newest first |
| GET | `/servers` | yes | known book servers (from the IRC user list) |

Requests to unmatched `/api` routes get `404 {"code":"not_found"}`; matched
routes called with the wrong HTTP method get `405 {"code":"method_not_allowed"}`.

## Errors

Every non-2xx body is `{"code":"…","message":"…"}`.

| HTTP | code |
|---|---|
| 400 | `bad_request` |
| 401 | `unauthorized` |
| 404 | `job_not_found`, `not_found` |
| 405 | `method_not_allowed` |
| 409 | `browser_session_active`, `queue_full` (3 queued per type), `not_cancellable` (job already finished) |
| 503 | `api_disabled` |

Job-level `error.code`: `timeout`, `server_unavailable`, `irc_connect_failed`,
`irc_disconnected`, `dcc_failed`, `parse_failed`, `cancelled`,
`browser_session_active`.

## Limits and behaviour

* One search and one download in flight at a time; up to 3 of each queued.
* Searches share the `--rate-limit` window with the browser UI.
* `--search-job-timeout` (120s) bounds the wait for the search bot;
  `--download-job-timeout` (10m) bounds the wait for the download bot's
  offer — the transfer itself then runs to completion.
* Files land in `<dir>/books/` by default, the same place the UI puts them —
  configurable with `--library-subdir` (empty puts them directly in `<dir>`).
