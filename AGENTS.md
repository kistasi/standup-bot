# AGENTS.md

This file provides guidance to agentic coding tools when working with code in this repository.

## Overview

`standup-bot` is an early-stage Go application that calls the Anthropic Claude API. Currently `main.go` is a single-shot proof-of-concept: it loads env vars, creates a client, and prints one Claude response. There is no standup logic yet — expect to build it out.

## Commands

```bash
go run .          # run the program
go build ./...    # compile
go test ./...     # run all tests (no tests exist yet)
go test -run TestName ./path  # run a single test
go mod tidy       # sync go.mod/go.sum after changing imports
```

Requires Go 1.26+.

## Configuration

- Requires `ANTHROPIC_API_KEY` in a `.env` file (loaded via `godotenv.Load()`; `.env` is gitignored). The Anthropic SDK reads this key automatically from the environment — `anthropic.NewClient()` takes no explicit key argument.
- Copy `.env.example` to `.env` to start. Note: `.env.example` currently contains what appears to be a real API key rather than a placeholder — treat it as compromised and rotate it.

## Notes

- The Claude model is referenced via SDK constants (e.g. `anthropic.ModelClaudeOpus4_6`), not raw strings.
- Dependencies in `go.mod` are marked `// indirect` because nothing is committed that imports them at the module level yet; running `go mod tidy` after adding real usage will promote the direct ones.
