# OpenAPI Skill Specification (Stub)

This document will capture the full contract for the built-in `http/openapi` skill, including:

- Input envelope schema (`spec`, `operationId`, params, auth, pagination, retry, dry_run)
- Output envelope schema (summaries vs CAS, pagination metadata)
- Built-in auth schemes (bearer, apiKey, basic, oauth2 client-credentials)
- Built-in pagination strategies (link, cursor, offset)
- Error codes (`EOPENAPI`, `EAUTH`, `EPAGINATION`, etc.)
- Dry-run request plan requirements

TODO: Port the relevant portions from the Core Profile spec and add detailed examples + golden fixtures.
