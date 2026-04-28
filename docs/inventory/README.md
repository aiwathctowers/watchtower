# Behavior Inventory

This directory catalogs the **behavioral contracts** of each business module — the user-observable invariants that must not change without explicit owner approval.

Each entry is a guard against silent regression. Modifying any contract or its guard test requires explicit approval from @Vadym.

## Module → file mapping

| Module | Inventory file | Code paths |
|---|---|---|
| Inbox Pulse | [inbox-pulse.md](inbox-pulse.md) | `internal/inbox/`, `WatchtowerDesktop/Sources/Views/Inbox/`, `WatchtowerDesktop/Sources/ViewModels/Inbox*.swift` |

(Other modules will be added as their inventories are written.)

## Protocol

1. Before changing code under any listed path, **read the corresponding inventory file**.
2. Identify whether the change touches any `<MODULE>-NN` contract.
3. If yes, **stop and ask the owner** before proceeding. Quote the affected ID.
4. If approved, change code + guard test + inventory entry + changelog **in one atomic commit**.

There are no pre-commit hooks, CI gates, or codeowner enforcement. Protection rests on four soft layers:

- Guard tests fail at `make test`.
- Test name prefix (`TestInbox01_…`, `TestDigest03_…`) is greppable.
- `// BEHAVIOR …` comment markers show up in diff.
- AI assistant reads inventory before touching covered code.
