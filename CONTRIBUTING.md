# Contributing to Glyph Call

Open an issue for a bug or feature proposal. For call or rendering bugs, include your OS, terminal, command flags, and whether the connection uses a local or public mailbox. Remove invite codes and private media before sharing logs.

Format Go changes with `gofmt` and run `go vet ./...` and `go test -race ./...`. On macOS, use `./build.sh` to verify the camera-enabled CLI build.

Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for commit messages, for example `fix(terminal): keep both panes visible after resize`.
