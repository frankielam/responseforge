# Contributing

Thanks for helping improve ResponseForge.

## Before opening a pull request

Please keep captured data, credentials, generated binaries, dependency directories, and unrelated local material out of commits. Runtime data belongs under `private/data`; local release artifacts belong under `private/dist`. Both are ignored by Git.

Run the checks relevant to your change:

```bash
go test ./...
go test -race ./...
go vet ./...

cd worker && npm ci && npm run typecheck
cd ../extension && npm ci && npm run typecheck && npm run build
```

If you change the public API, configuration, storage behavior, extension settings, or LLM behavior, update `README.md`, `docs/USAGE.md`, or `PRD.md` as appropriate. Do not include real API keys, bearer tokens, cookies, authorization headers, captured response bodies, or signed URLs in examples or tests.

## Pull requests

- Explain the user-visible behavior and the reason for the change.
- Keep unrelated formatting or generated files out of the patch.
- Add or update tests for behavior changes.
- Mention any compatibility or migration impact.
- Confirm that `private/` and local environment files are not included.

Small, focused pull requests are easier to review and merge.
