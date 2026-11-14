# Contributing to Glyph Call

Open an issue for a bug or feature proposal. For call or rendering bugs, include your OS, terminal, command flags, and whether the connection uses a local or public mailbox. Remove invite codes and private media before sharing logs.

Format Go changes with `gofmt` and run `go vet ./...` and `go test -race ./...`. On macOS, use `./build.sh` to verify the camera-enabled CLI build.

## Commit messages and documentation

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).
Write each subject in English, start the summary with a verb, and aim for
72 characters or fewer. Use `type: summary` or `type(scope): summary`.

| Type | Use for |
| --- | --- |
| `feat` | New functionality |
| `fix` | Bug fixes |
| `docs` | Documentation |
| `build` | Build tools, dependencies, and packaging |
| `ci` | CI configuration and workflows |
| `refactor` | Code changes that preserve behavior |
| `perf` | Performance improvements |
| `test` | Tests |
| `chore` | Other project maintenance |

For example, use `docs: clarify the quick start` or
`fix(terminal): restore the cursor on exit`. Mark breaking changes with
`!` before the colon or a `BREAKING CHANGE:` footer.

Write project documentation in English. Use the README for the project
overview, prerequisites, and quick start. Link to the relevant guides from
the README and put longer guides in `docs/`. Use descriptive headings,
relative links for repository files, and alt text for screenshots.
