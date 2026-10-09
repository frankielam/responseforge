# ResponseForge

Open-source capture pipeline: a Chromium extension sends matching `fetch` and `XMLHttpRequest` responses to an HTTP server. The server stores structured rows when a declarative rule matches, and otherwise stores the whole payload as one raw row. The Go server and the Cloudflare Worker implement the same ingest API.

The extension does not log in, does not type passwords, and does not upload `Cookie`, `Authorization`, or `Set-Cookie`.

New to the project? Start with [`docs/QUICKSTART.md`](docs/QUICKSTART.md). AI coding agents should read [`AGENTS.md`](AGENTS.md) before running commands.

## Shortest paths

### 1. Go server, CSV

```bash
go run ./cmd/server
```

Data is written to `./private/data` (`private/data/raw.csv`, `private/data/tables/<table>.csv`, `private/data/rules.json`). `STORAGE` defaults to `csv` when it is unset. The `private/` directory is intentionally ignored by Git and is not part of the public source tree.

When upgrading an older checkout, move its existing `data/` directory to `private/data/`, or set `DATA_DIR` explicitly to the old location before starting the server.

Docker:

```bash
docker compose up server
```

### 2. Go server and PostgreSQL

```bash
docker compose --profile db up
```

Compose starts PostgreSQL with the `db` profile. The server container uses PostgreSQL when the `postgres` hostname is present, and it exits if that connection fails. It does not fall back to CSV. Defaults are user `edf`, password `edf`, database `edf`. Override `POSTGRES_PASSWORD` together with `DATABASE_URL`.

### 3. Cloudflare Workers and D1

```bash
cd worker
npm install
npx wrangler d1 create responseforge   # put the id in wrangler.toml
npx wrangler d1 migrations apply DB --local
npx wrangler dev
```

Deploy:

```bash
npx wrangler d1 migrations apply DB --remote
npx wrangler secret put INGEST_TOKEN
npx wrangler secret put LLM_API_KEY
npx wrangler secret put LLM_BASE_URL
npx wrangler secret put LLM_MODEL
npx wrangler deploy
```

Leave `LLM_BASE_URL` empty to disable the model. The D1 binding name is `DB`. Worker request bodies are capped at 1 MB and then stored with `truncated: true`.

### 4. Load the extension

```bash
cd extension
npm install
npm run build
```

In Chrome or Edge, open `chrome://extensions`, enable Developer mode, and choose **Load unpacked**. Select `extension/.output/chrome-mv3`.

Open the extension options. The server URL defaults to `http://127.0.0.1:8080`. Add URL patterns such as `https://partner.example/affiliate/creators*`, and set a bearer token when the server has `INGEST_TOKEN`. Point the same options page at a Worker URL to switch servers. No other protocol change is required.

### Local and private files

Captured response bodies, request URLs, generated rules, job records, local release artifacts, and unrelated local material belong under `private/`. Do not publish that directory. The root `.env` file is also ignored and must never contain committed credentials. Use `.env.example` as the public template.

## HTTP API

All routes except `GET /healthz` require `Authorization: Bearer <INGEST_TOKEN>` when that variable is set. If LLM is enabled, the Worker always requires the token; the Go server permits an un-tokened LLM only from a loopback client.

`POST /api/v1/ingest` accepts:

```json
{
  "protocol": 1,
  "url": "https://shop.example/api/orders/1842",
  "method": "GET",
  "status": 200,
  "content_type": "application/json",
  "body": "{\"data\":{\"items\":[]}}",
  "captured_at": "2026-10-04T02:16:00+08:00",
  "truncated": false
}
```

A stored event returns `202` with `mode` of `parsed` or `raw`, and `llm` of `skipped`, `scheduled`, or `disabled`. Text content types (`json`, `html`, `xml`, `text/*`, `csv`) are accepted. Other types return `415`.

`GET /api/v1/rules` lists rules. `PUT /api/v1/rules` saves one rule and, when `sample_body` is included, replays it. `POST /api/v1/rules/regenerate` asks the configured model to rewrite a pattern.

Rules are declarative: JSONPath, a CSS selector plus an optional `attr`, or one named-group regular expression. The server adds `_captured_at`, `_source_url`, `_rule_id`, and `_rule_version`.

## TikTok Shop Affiliate Partner

This is a configured scenario, not a built-in scraper. You log in yourself. An external RPA tool, or you, can browse creator and merchant pages. The extension only forwards responses whose URLs match patterns you entered.

Example rules for `tiktok_creators` and `tiktok_merchants` are in [`examples/tiktok-rules.json`](examples/tiktok-rules.json). Replace the placeholder patterns with the endpoints from your own signed-in dashboard, then `PUT` each rule. `creator_id` and `merchant_id` are required. Repeated visits append rows; the server does not deduplicate them. With no rule, the response is still stored as one raw row.

```bash
jq -c '.[0]' examples/tiktok-rules.json | curl -sS -X PUT http://127.0.0.1:8080/api/v1/rules \
  -H 'content-type: application/json' -d @-
```

## Optional LLM

Set `LLM_BASE_URL`, `LLM_API_KEY`, and `LLM_MODEL` to an OpenAI-compatible API. When no rule matches, the server stores the raw row, then generates a rule in the background. The same URL pattern is not sent to the model again while a job is running or after a rule is saved. A parse failure stores the raw row and asks the model to repair the rule. If repair fails, the previous rule stays in place.

In PostgreSQL or D1, the first successful generation can also create a table. The server generates the `CREATE TABLE IF NOT EXISTS` statement from the validated rule; model-supplied SQL is never executed. Existing tables must already contain every rule field plus the four system columns.

## Capture limits

The extension truncates response bodies at 2 MB (configurable, hard cap 10 MB) and sets `truncated: true`. If you enable response headers, `Cookie`, `Authorization`, and `Set-Cookie` are removed before upload. The ingest schema does not store that header map, so it cannot land in `raw.csv` or `raw_events`. The upload queue keeps at most 100 items or 20 MB and drops the oldest first. Failed uploads are retried. Document navigations, images, audio, video, WebSockets, and requests that do not use `fetch` or XHR are not captured.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
(cd worker && npm ci && npm run typecheck)
(cd extension && npm ci && npm run typecheck && npm run build)
```

Copy [`.env.example`](.env.example) when you want local environment variables. Do not commit real tokens or model keys.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the development checks and pull request expectations. See [`SECURITY.md`](SECURITY.md) for handling captured data, credentials, and vulnerability reports.

## License

MIT. See [LICENSE](LICENSE).
