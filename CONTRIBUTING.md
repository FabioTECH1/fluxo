# Contributing to Fluxo

## Getting started

```sh
git clone https://github.com/FabioTECH1/fluxo.git
cd fluxo

# Install frontend dependencies
cd ui && npm install && cd ..

# Build frontend (required before Go build — embeds dist/ into binary)
cd ui && npm run build && cd ..

# Build Go binary
go build -o fluxo ./cmd/fluxo

# Run (serves HTTPS with self-signed cert on :9595)
./fluxo

# Plain HTTP for development
FLUXO_USE_HTTP=1 ./fluxo
```

See [AGENTS.md](AGENTS.md) for the full architecture overview, API reference, and coding conventions.

## Before submitting a PR

1. **Typecheck** — `cd ui && npx vue-tsc -b --noEmit`
2. **Build frontend** — `cd ui && npm run build`
3. **Compile Go** — `go build -o /dev/null ./cmd/fluxo`
4. **Vet** — `go vet ./...`
5. **Run the binary** — verify the change works end-to-end

## Code conventions

- Follow the existing patterns in neighboring files
- Go: `syscmd.Run()` for all external commands — never `os/exec` directly outside `internal/syscmd/`
- Vue: use the existing components (`BaseModal`, `Card`, `AppButton`, `DataTable`, etc.) before writing raw HTML
- Dark mode is built into every reusable component — add `dark:` variants there, not in page-level views
- Keep the existing naming conventions, file structure, and import style

## Issue reporting

- Search existing issues before opening a new one
- Include steps to reproduce, expected behavior, and actual behavior
- Let us know your OS, Go version, and Node version

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
