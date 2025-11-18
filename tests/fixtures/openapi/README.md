# OpenAPI Skill Test Fixtures

This directory contains golden test fixtures for the OpenAPI skill as specified in SPEC-018.

## Directory Structure

```
tests/fixtures/openapi/
├── inputs/          # Request input JSON files
├── outputs/         # Expected response JSON files
├── specs/           # OpenAPI specification files
└── README.md        # This file
```

## Fixture Types

### Input Fixtures (`inputs/`)

Test input scenarios for the OpenAPI skill:

- `get-pet-success.json` - Simple GET request with path parameter
- `list-pets-paginated.json` - GET request with pagination configuration
- `create-pet.json` - POST request with request body
- `error-missing-param.json` - Missing required path parameter (EARG)
- `error-invalid-operation.json` - Invalid operationId (EOPENAPI)
- `dry-run.json` - Dry-run mode request

### Output Fixtures (`outputs/`)

Expected responses for test scenarios:

- `dry-run-expected.json` - Expected dry-run output with redacted secrets

### Spec Fixtures (`specs/`)

OpenAPI specifications for testing:

- `petstore.yaml` - Simple petstore API with authentication, CRUD operations

## Usage

### Manual Testing

```bash
# Test dry-run mode
cat tests/fixtures/openapi/inputs/dry-run.json | \
  go run skills/http_openapi/main.go

# Compare with expected output
cat tests/fixtures/openapi/inputs/dry-run.json | \
  go run skills/http_openapi/main.go | \
  jq -S . > actual.json
diff tests/fixtures/openapi/outputs/dry-run-expected.json actual.json
```

### Automated Testing

```bash
# Run all fixture tests
go test ./tests/fixtures/openapi/...

# Run specific fixture
go test -run TestFixture_DryRun
```

## Test Scenarios Covered

### Success Cases
- ✅ Simple GET request with path parameters
- ✅ POST request with JSON body
- ✅ Pagination with auto-detection
- ✅ Dry-run mode with secret redaction
- ✅ Authentication (Bearer token)

### Error Cases
- ✅ Missing required parameters (EARG)
- ✅ Invalid operationId (EOPENAPI)
- ⚠️  Authentication failures (401) - requires mock server
- ⚠️  Rate limiting (429) - requires mock server
- ⚠️  Server errors (5xx) - requires mock server

### Advanced Features
- ✅ Pagination configuration
- ✅ Retry configuration
- ✅ Request body serialization
- ✅ Secret redaction in outputs
- ⚠️  OAuth2 flow - requires token endpoint
- ⚠️  Multi-page aggregation - requires mock server

## Adding New Fixtures

1. Create input file in `inputs/` with descriptive name
2. Create expected output in `outputs/` if needed
3. Add test spec in `specs/` if needed
4. Update this README with the new scenario
5. Add test case in `fixtures_test.go`

## Validation

All fixtures should:
- Use valid JSON format
- Include all required fields per SPEC-013
- Match the OpenAPI skill input schema
- Have corresponding output fixtures for deterministic scenarios
- Include comments explaining non-obvious scenarios
