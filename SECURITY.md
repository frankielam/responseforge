# Security policy

## Scope

This project captures HTTP response bodies from browser requests and can optionally send samples to a configured LLM. Treat captured data, request URLs, bearer tokens, cookies, and model credentials as sensitive.

The extension does not log in for the user and is not intended to bypass authentication or access controls. Only capture traffic that you are authorized to access.

## Local data handling

- Runtime data is stored under `private/data` by default.
- The `private/` directory, `.env`, and worker local variables are ignored and must not be committed.
- Do not paste real captured bodies, signed URLs, cookies, authorization headers, or API keys into issues, pull requests, tests, or documentation.
- If LLM support is enabled, review the configured endpoint and the sample data policy before sending production responses.
- Rotate any credential that was accidentally committed or shared. Removing it from a later commit does not invalidate it.

## Reporting a vulnerability

Please do not open a public issue for an undisclosed vulnerability. Contact the maintainer privately with:

- a short description and affected component;
- reproduction steps or a minimal, sanitized proof of concept;
- the impact and any suggested mitigation.

Please allow reasonable time for investigation and a fix before public disclosure. If no private contact channel is configured for the repository, use the repository owner's security contact rather than publishing sensitive details.
