<div align="center">

<img src="https://storage.googleapis.com/sketch-assets/sketch-logo.png" alt="Sketch Logo" width="300"/>

# Sketch

[![Go Reference](https://pkg.go.dev/badge/sketch.dev.svg)](https://pkg.go.dev/sketch.dev)
[![Discord](https://img.shields.io/discord/1362869091156758752?logo=discord&logoColor=white&label=Discord)](https://discord.gg/6w9qNRUDzS)
[![GitHub Workflow Status](https://github.com/boldsoftware/sketch/actions/workflows/go_test.yml/badge.svg)](https://github.com/boldsoftware/sketch/actions/workflows/go_test.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/boldsoftware/sketch/blob/main/LICENSE)

**Sketch is an agentic coding tool. It draws the 🦉**

</div>

## 🚀 Overview

Sketch runs in your terminal, has a web UI, understands your code, and helps
you get work done. To keep your environment pristine, sketch starts a docker
container and outputs its work onto a branch in your host git repository.

Sketch supports multiple LLM providers and models, including Claude 4, GPT-4.1, Google Gemini 2.5, and specialized coding models like Qwen3-Coder. It works with most programming environments, but has extra goodies for Go.

<img src="https://storage.googleapis.com/sketch-assets/screenshot.jpg" alt="Sketch Screenshot" width="800"/>

## 📋 Quick Start

### Install via Homebrew

```sh
brew install boldsoftware/tap/sketch
```

Update to the latest version by running `brew upgrade boldsoftware/tap/sketch`.

### Install via GitHub Releases

Grab the most recent [nightly release](https://github.com/boldsoftware/sketch/releases).

Update by running `sketch -update`.

### Build from source

Clone this repo, and then run:

```sh
$ make
$ ./sketch
```

### Choose Your Model

Sketch supports multiple AI models. Use `-model` to select one:

```sh
# Use Claude 4 (default)
sketch

# Use GPT-4.1
sketch -model gpt4.1

# Use Google Gemini 2.5
sketch -model gemini

# Use Qwen3-Coder for specialized coding tasks
sketch -model qwen

# List all available models
sketch -list-models
```

## 🔧 Requirements

Currently, Sketch runs on MacOS and Linux. It uses Docker for containers.

| Platform | Installation                                                               |
| -------- | -------------------------------------------------------------------------- |
| MacOS    | `brew install colima` (or [OrbStack](https://orbstack.dev/) or [Docker Desktop](https://www.docker.com/products/docker-desktop/))                         |
| Linux    | `apt install docker.io` (or equivalent for your distro)                    |
| WSL2     | Install Docker Desktop for Windows (docker entirely inside WSL2 is tricky) |

The [sketch.dev](https://sketch.dev) service is used to provide access
to an LLM service and give you a way to access the web UI from anywhere.

### Key Options

- **`-open=false`**: Use CLI-only mode (no browser)
- **`-unsafe`**: Run without Docker container (use with caution)
- **`-skaband-addr=""`**: Run entirely offline with your own API key
- **`-max-dollars 5`**: Set spending limits per turn
- **`-one-shot "implement user auth"`**: Run a single command and exit
- **`-model qwen`**: Choose your preferred AI model
- **`-branch-prefix myfeature/`**: Customize git branch naming

### Running Offline

You can run Sketch entirely without the sketch.dev service by providing your own API keys:

```sh
# With Anthropic Claude
ANTHROPIC_API_KEY=your-key sketch -skaband-addr=""

# With OpenAI GPT models
OPENAI_API_KEY=your-key sketch -skaband-addr="" -model gpt4.1

# With Google Gemini
GEMINI_API_KEY=your-key sketch -skaband-addr="" -model gemini
```

This gives you full control over your data and API usage while still benefiting from Sketch's capabilities.

## 🤝 Community & Feedback

- **Discord**: Join our community at [https://discord.gg/6w9qNRUDzS](https://discord.gg/6w9qNRUDzS)
- **GitHub Issues**: Submit feedback at [https://github.com/boldsoftware/sketch/issues](https://github.com/boldsoftware/sketch/issues)

## 📖 User Guide

### Getting Started

Start Sketch by running `sketch` in a Git repository. It will open your browser to the Sketch chat interface, but you can also use the CLI interface. Use `-open=false` if you want to use just the CLI interface.

Ask Sketch about your codebase or ask it to implement a feature. It may take a little while for Sketch to do its work, so hit the bell (🔔) icon to enable browser notifications. We won't spam you or anything; it will notify you
when the Sketch agent's turn is done, and there's something to look at.

### How Sketch Works

Sketch uses a two-layer architecture:

**Outer Sketch** (your machine):
- Runs the CLI and web UI
- Manages Docker containers
- Handles git integration and branch management

**Inner Sketch** (Docker container):
- Runs the AI agent in an isolated sandbox
- Executes code changes and tool calls
- Makes git commits that are pushed back to your host

When you start Sketch, it:

1. Creates a custom Dockerfile for your project
2. Builds a container with your codebase
3. Starts the AI agent inside the container
4. Sets up git integration between container and host

This design provides several benefits:
- **Safety**: The AI can't damage your host system
- **Isolation**: Each session runs in its own clean environment
- **Parallelism**: Run multiple Sketch sessions simultaneously
- **Reproducibility**: Consistent environment for every run

The AI agent uses tool calls including shell commands, file operations, git commands, browser automation, and code analysis tools to understand and modify your codebase.

### Getting Your Git Changes Out

<!-- TODO: git picture -->

Sketch is trained to make Git commits. When those happen, they are
automatically pushed to the git repository where you started sketch with branch
names `sketch/*`.

**Finding Sketch branches:**

```sh
git branch -a --sort=creatordate | grep sketch/ | tail
```

The UI keeps track of the latest branch it pushed and displays it prominently. You can use standard Git workflows to pull those branches into your workspace:

```sh
git cherry-pick $(git merge-base origin/main sketch/foo)
```

or merge the branch

```sh
git merge sketch/foo
```

or reset to the branch

```sh
git reset --hard sketch/foo
```

Ie use the same workflows you would if you were pulling in a friend's Pull Request.

**Advanced:** You can ask Sketch to `git fetch sketch-host` and rebase onto another commit. This will also fetch where you started Sketch, and we do a bit of "git fetch refspec configuration" to make `origin/main` work as a git reference.

Don't be afraid of asking Sketch to help you rebase, merge/squash commits, rewrite commit messages, and so forth; it's good at it!

### Reviewing Diffs

The diff view shows you changes since Sketch started. Leaving comments on lines
adds them to the chat box, and, when you hit Send (at the bottom of the page), Sketch goes to work addressing your
comments.

### Connecting to Sketch's Container

You can interact directly with the container in three ways:

1. **Web UI Terminal**: Use the "Terminal" tab in the UI
2. **SSH**: Look at the startup logs or click the information icon to see a command like `ssh sketch-ilik-eske-tcha-lott`.
   We have automatically configured your SSH configuration to make these special hostnames work.
3. **Visual Studio Code**: Look for a command line or magic link behind the information icon, or when Sketch starts up. This starts a new VSCode session "remoted into" the container. You
   can edit the code, use the terminal, review diffs, and so forth.

Using SSH (and/or VSCode) allows you to forward ports from the container to your machine. For example, if you want to start your development webserver, you can do something like this:

```sh
# Forward container port 8888 to local port 8000
ssh -L8000:localhost:8888 sketch-ilik-epor-tfor-ward go run ./cmd/server
```

This makes `http://localhost:8000/` on your machine point to `localhost:8888` inside the container.

### Using Browser Tools

Sketch includes powerful browser automation capabilities:

- **Screenshots**: Take screenshots of web pages or specific elements
- **Visual analysis**: Send images to the LLM for analysis and feedback
- **Web interaction**: Navigate pages, click buttons, fill forms, and interact with web UIs
- **Development workflow**: Perfect for testing web applications and reviewing UI changes

This is especially useful when working on web applications - you can ask Sketch to start your dev server, take screenshots, and provide visual feedback on your changes.

## ❓ FAQ

### "No space left on device"

Docker images, containers, and so forth tend to pile up. Ask Docker to prune unused images and containers:

```sh
docker system prune -a
```

## 🛠️ Development

[![Go Reference](https://pkg.go.dev/badge/sketch.dev.svg)](https://pkg.go.dev/sketch.dev)

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## 📄 Open Source

Sketch is open source.
It is right here in this repository!
Have a look around and mod away.
