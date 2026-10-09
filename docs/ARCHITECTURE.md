# ResponseForge architecture

ResponseForge has three cooperating parts:

1. The Chromium extension observes configured `fetch` and `XMLHttpRequest` responses in the browser and queues sanitized payloads for upload.
2. The Go server accepts the ingest protocol, matches each URL to a declarative rule, parses JSON/HTML/XML/text content, and writes either structured rows or a raw fallback row.
3. The Cloudflare Worker implements the same ingest and rule workflow using D1 for persistent storage.

## Request flow

```text
Browser request
    -> extension URL matcher
    -> response-size limit and sensitive-header filtering
    -> local upload queue
    -> Go server or Cloudflare Worker
    -> rule match
       -> parse structured rows -> CSV/PostgreSQL/D1
       -> no match or parse failure -> raw row, optional background LLM rule generation
```

The extension never needs the user's website credentials. It forwards responses only for patterns configured by the user. The server adds `_captured_at`, `_source_url`, `_rule_id`, and `_rule_version` to structured rows.

## Storage

- CSV mode stores local rules and captured data in `private/data` by default.
- PostgreSQL mode stores rules, raw events, and parsed rows in the database.
- The Worker stores the equivalent records in D1.

Generated release files are written to `private/dist`. Dependencies and build caches remain in their tool-specific directories and are ignored by Git.

## LLM boundary

LLM support is optional. It is only used to generate or repair a declarative rule when no usable rule is available. Model output is validated before use, and server-side SQL is generated from the validated rule rather than executed from model output. Keep the endpoint, key, and sample-data policy under the operator's control.
