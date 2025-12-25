---
name: auth-route-tester
description: Test API routes after implementation - verify authentication, data handling, database records, response formats. Use after creating or modifying routes.
---

Test routes for complete functionality.

## Test Coverage

1. **Authentication**: Verify auth requirements work
2. **Input validation**: Test valid and invalid payloads
3. **Database operations**: Confirm records created/updated correctly
4. **Response format**: Check status codes, response structure
5. **Error handling**: Test error cases return proper responses

## Process

1. Identify route requirements
2. Prepare test payloads (valid + invalid)
3. Execute requests with proper auth
4. Verify database state
5. Check response matches expectations

## Output

- Test results summary
- Issues found
- Implementation review suggestions

Full docs: `~/repos/personal/agentctl/configs/agents/auth-route-tester.md`
