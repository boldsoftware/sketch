# Sketch

Sketch is an agentic coding tool focused on the Go programming language.
Sketch runs in your terminal, has a web UI, understands your code, and helps you get work done.
To keep your environment pristine, sketch starts a docker container and outputs
its work onto a branch in your host git repository.

To get started:

```sh
go install sketch.dev/cmd/sketch@latest
sketch
```

## Requirements

Currently sketch runs on macOS and linux.
It uses docker/colima for containers.

macOS: `brew install colima`
linux: `apt install docker.io` (or equivalent for your distro)

The [sketch.dev](https://sketch.dev) service is used to provide access
to an LLM service and give you a way to access the web UI from anywhere.

## Feedback/discussion

We have a discord server to discuss sketch.

Join if you want! https://discord.gg/R82YagTASx

## Development

[![Go Reference](https://pkg.go.dev/badge/sketch.dev.svg)](https://pkg.go.dev/sketch.dev)

See [CONTRIBUTING.md](CONTRIBUTING.md)

## Open Source

Sketch is open source.
It is right here in this repository!
Have a look around and mod away.

If you want to run sketch entirely without the sketch.dev service, you can
set the flag -skaband-addr="" and then provide an `ANTHROPIC_API_KEY`
environment variable. (More LLM services coming soon!)
