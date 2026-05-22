# Frontend Verification

Foxctl has several TypeScript surfaces with different runtimes. Use the
focused frontend gate when changing shared DTOs, GUI orchestration, foxterm, or
the GUI auth gateway:

```bash
bun run check:frontend
```

That command runs:

- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/gui-agent lint`
- `bun run --cwd packages/gui-agent build`
- `bun run --cwd packages/gui-agent test`
- `bun run --cwd packages/foxterm typecheck`
- `bun run --cwd packages/gui-auth-gateway typecheck`

For unused-code cleanup, run:

```bash
bun run unused:frontend
```

This runs the frontend gate, `oxlint packages/`, and the targeted dead-export
report:

```bash
bun run dead:frontend
```

The TypeScript packages listed above enable `noUnusedLocals` and
`noUnusedParameters`, so unused imports, locals, and parameters fail the package
checks instead of relying on informal review.

`dead:frontend` is intentionally targeted to active frontend modules; it does
not replace caller search or DAG grep before deleting broader exports.
