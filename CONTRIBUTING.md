# Contributing to WSBro

Thanks for your interest in contributing!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/your-username/wsbro.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run tests: `go test ./server/ -v`
6. Commit: `git commit -m "Add your feature"`
7. Push: `git push origin feature/your-feature`
8. Open a Pull Request

## Development Setup

```bash
go run .
```

Server starts on `http://localhost:6001`. Dashboard at `http://localhost:6001/dashboard`.

## Running Tests

```bash
go test ./server/ -v
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions small and focused
- Add tests for new features
- Use `zerolog` for logging, not `fmt.Println`

## Reporting Issues

- Use GitHub Issues
- Include steps to reproduce
- Include Go version (`go version`)
- Include relevant logs

## Pull Requests

- Keep PRs focused on a single change
- Update documentation if needed
- Add tests for new functionality
- Make sure all existing tests pass