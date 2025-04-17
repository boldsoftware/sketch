all: install check

install:
	npm ci

# TypeScript type checking
# Note: The actual esbuild bundling happens in esbuild.go
check:
	npx tsc --noEmit

clean:
	rm -rf node_modules
