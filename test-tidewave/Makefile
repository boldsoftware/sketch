# Makefile for Tidewave test environment

.PHONY: help setup check server test clean

help: ## Show this help message
	@echo "Tidewave Test Environment"
	@echo "========================"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

setup: ## Install dependencies and compile
	mix local.hex --force
	mix deps.get
	mix compile

check: ## Verify setup is correct
	./setup-check.sh

server: setup ## Start the Phoenix server
	mix phx.server

test: ## Run MCP integration tests (requires server running)
	./test-mcp.sh

manual: ## Run manual MCP protocol test
	./manual-test.sh

clean: ## Clean compiled files and dependencies
	mix deps.clean --all
	rm -rf _build
	rm -rf deps

install-asdf: ## Install Erlang and Elixir using ASDF
	@echo "Installing ASDF if not present..."
	@if [ ! -d "$$HOME/.asdf" ]; then \
		git clone https://github.com/asdf-vm/asdf.git ~/.asdf --branch v0.14.0; \
		echo '. ~/.asdf/asdf.sh' >> ~/.bashrc; \
		echo '. ~/.asdf/completions/asdf.bash' >> ~/.bashrc; \
	fi
	@echo "Installing ASDF plugins..."
	@. ~/.asdf/asdf.sh && asdf plugin add erlang https://github.com/asdf-vm/asdf-erlang.git || true
	@. ~/.asdf/asdf.sh && asdf plugin add elixir https://github.com/asdf-vm/asdf-elixir.git || true
	@echo "Installing Erlang and Elixir..."
	@. ~/.asdf/asdf.sh && asdf install erlang 26.2.5.2
	@. ~/.asdf/asdf.sh && asdf install elixir 1.17.3-otp-26
	@. ~/.asdf/asdf.sh && asdf local erlang 26.2.5.2
	@. ~/.asdf/asdf.sh && asdf local elixir 1.17.3-otp-26
	@echo "✅ ASDF installation complete! Please restart your shell or run: source ~/.bashrc"
