# Contributing to Glyph Call

Open an issue for a bug or feature proposal. For call or rendering bugs, include your OS, terminal, command flags, and whether the connection uses a local or public mailbox. Remove invite codes and private media before sharing logs.

Format Go changes with `gofmt` and run `go vet ./...` and `go test -race ./...`. On macOS, use `./build.sh` to verify the camera-enabled CLI build.

## Commit messages and documentation

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

| Type | Use for |
| --- | --- |
| `feat` | New user-facing capability |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `build` | Build system, dependencies, packaging |
| `refactor` | Code change that is neither feat nor fix |
| `test` | Tests only |
| `chore` | Maintenance that does not change product behavior |

Format: `<type>(optional-scope): <imperative summary>`

Examples:

- `feat(terminal): keep both panes visible after resize`
- `fix(webrtc): retry ICE after temporary network loss`
- `docs: clarify host and join flow`

Keep the subject under ~72 characters, use the imperative mood, and write in English.

All project documentation (`README`, `docs/`, `CONTRIBUTING`, and contributor-facing comments) must be in English. Do not add localized documentation files.
