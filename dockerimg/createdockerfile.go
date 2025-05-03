package dockerimg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"slices"
	"strings"
	"text/template"

	"sketch.dev/llm"
	"sketch.dev/llm/conversation"
)

func hashInitFiles(initFiles map[string]string) string {
	h := sha256.New()
	for _, path := range slices.Sorted(maps.Keys(initFiles)) {
		fmt.Fprintf(h, "%s\n%s\n\n", path, initFiles[path])
	}
	fmt.Fprintf(h, "docker template\n%s\n", dockerfileDefaultTmpl)
	return hex.EncodeToString(h.Sum(nil))
}

// DefaultImage is intended to ONLY be used by the pushdockerimg.go script.
func DefaultImage() (name, dockerfile, tag string) {
	return dockerImgName, dockerfileBase, dockerfileBaseHash()
}

const (
	dockerImgRepo = "boldsoftware/sketch"
	dockerImgName = "ghcr.io/" + dockerImgRepo
)

func dockerfileBaseHash() string {
	h := sha256.New()
	io.WriteString(h, dockerfileBase)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

const tmpSketchDockerfile = "tmp-sketch-dockerfile"

const dockerfileBase = `FROM golang:1.24-bookworm

# Switch from dash to bash by default.
SHELL ["/bin/bash", "-euxo", "pipefail", "-c"]

# attempt to keep package installs lean
RUN printf '%s\n' \
      'path-exclude=/usr/share/man/*' \
      'path-exclude=/usr/share/doc/*' \
      'path-exclude=/usr/share/doc-base/*' \
      'path-exclude=/usr/share/info/*' \
      'path-exclude=/usr/share/locale/*' \
      'path-exclude=/usr/share/groff/*' \
      'path-exclude=/usr/share/lintian/*' \
      'path-exclude=/usr/share/zoneinfo/*' \
    > /etc/dpkg/dpkg.cfg.d/01_nodoc

RUN apt-get update; \
	apt-get install -y --no-install-recommends \
		git jq sqlite3 npm nodejs gh ripgrep fzf python3 curl vim chromium && \
	apt-get clean && \
	rm -rf /var/lib/apt/lists/* && \
	rm -rf /usr/share/{doc,doc-base,info,lintian,man,groff,locale,zoneinfo}/*

ENV PATH="$GOPATH/bin:$PATH"

# While these binaries install generally useful supporting packages,
# the specific versions are rarely what a user wants so there is no
# point polluting the base image module with them.

RUN go install golang.org/x/tools/cmd/goimports@latest; \
	go install golang.org/x/tools/gopls@latest; \
	go install mvdan.cc/gofumpt@latest; \
	go clean -cache -testcache -modcache

ENV GOTOOLCHAIN=auto
ENV SKETCH=1

RUN mkdir -p /root/.cache/sketch/webui
`

const dockerfileFragment = `
ARG GIT_USER_EMAIL
ARG GIT_USER_NAME

RUN git config --global user.email "$GIT_USER_EMAIL" && \
    git config --global user.name "$GIT_USER_NAME" && \
    git config --global http.postBuffer 524288000

LABEL sketch_context="{{.InitFilesHash}}"
COPY . /app
RUN rm -f /app/` + tmpSketchDockerfile + `

WORKDIR /app{{.SubDir}}
RUN if [ -f go.mod ]; then go mod download; fi

# Switch to lenient shell so we are more likely to get past failing extra_cmds.
SHELL ["/bin/bash", "-uo", "pipefail", "-c"]

{{.ExtraCmds}}

# Switch back to strict shell after extra_cmds.
SHELL ["/bin/bash", "-euxo", "pipefail", "-c"]

CMD ["/bin/sketch"]
`

var dockerfileDefaultTmpl = "FROM " + dockerImgName + ":" + dockerfileBaseHash() + "\n" + dockerfileFragment

func readPublishedTags() ([]string, error) {
	req, err := http.NewRequest("GET", "https://ghcr.io/token?service=ghcr.io&scope=repository:"+dockerImgRepo+":pull", nil)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		return nil, fmt.Errorf("token: %d: %s: %w", res.StatusCode, body, err)
	}
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &tokenBody); err != nil {
		return nil, fmt.Errorf("token: %w: %s", err, body)
	}

	req, err = http.NewRequest("GET", "https://ghcr.io/v2/"+dockerImgRepo+"/tags/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	body, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		return nil, fmt.Errorf("tags: %d: %s: %w", res.StatusCode, body, err)
	}
	var tags struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("tags: %w: %s", err, body)
	}
	return tags.Tags, nil
}

func checkTagExists(tag string) error {
	tags, err := readPublishedTags()
	if err != nil {
		return fmt.Errorf("check tag exists: %w", err)
	}
	for _, t := range tags {
		if t == tag {
			return nil // found it
		}
	}
	return fmt.Errorf("check tag exists: %q not found in %v", tag, tags)
}

// createDockerfile creates a Dockerfile for a git repo.
// It expects the relevant initFiles to have been provided.
// If the sketch binary is being executed in a sub-directory of the repository,
// the relative path is provided on subPathWorkingDir.
func createDockerfile(ctx context.Context, srv llm.Service, initFiles map[string]string, subPathWorkingDir string) (string, error) {
	if subPathWorkingDir == "." {
		subPathWorkingDir = ""
	} else if subPathWorkingDir != "" && subPathWorkingDir[0] != '/' {
		subPathWorkingDir = "/" + subPathWorkingDir
	}
	toolCalled := false
	var dockerfileExtraCmds string
	runDockerfile := func(ctx context.Context, input json.RawMessage) (string, error) {
		// TODO: unmarshal straight into a struct
		var m map[string]any
		if err := json.Unmarshal(input, &m); err != nil {
			return "", fmt.Errorf(`input=%[1]v (%[1]T), wanted a map[string]any, got: %w`, input, err)
		}
		var ok bool
		dockerfileExtraCmds, ok = m["extra_cmds"].(string)
		if !ok {
			return "", fmt.Errorf(`input["extra_cmds"]=%[1]v (%[1]T), wanted a string`, m["path"])
		}
		toolCalled = true
		return "OK", nil
	}

	convo := conversation.New(ctx, srv)

	convo.Tools = []*llm.Tool{{
		Name:        "dockerfile",
		Description: "Helps define a Dockerfile that sets up a dev environment for this project.",
		Run:         runDockerfile,
		InputSchema: llm.MustSchema(`{
  "type": "object",
  "required": ["extra_cmds"],
  "properties": {
    "extra_cmds": {
      "type": "string",
      "description": "Extra commands to add to the dockerfile."
    }
  }
}`),
	}}

	// TODO: it's basically impossible to one-shot a python env. We need an agent loop for that.
	// Right now the prompt contains a set of half-baked workarounds.

	// If you want to edit the model prompt, run:
	//
	//	go test ./dockerimg -httprecord ".*" -rewritewant
	//
	// Then look at the changes with:
	//
	//	git diff dockerimg/testdata/*.dockerfile
	//
	// If the dockerfile changes are a strict improvement, commit all the changes.
	msg := llm.Message{
		Role: llm.MessageRoleUser,
		Content: []llm.Content{{
			Type: llm.ContentTypeText,
			Text: `
Call the dockerfile tool to create a Dockerfile.
The parameters to dockerfile fill out the From and ExtraCmds
template variables in the following Go template:

` + "```\n" + dockerfileBase + dockerfileFragment + "\n```" + `

In particular:
- Assume it is primarily a Go project.
- Python env setup is challenging and often no required, so any RUN commands involving python tooling should be written to let docker build continue if there is a failure.
- Include any tools particular to this repository that can be inferred from the given context.
- Append || true to any apt-get install commands in case the package does not exist.
- MINIMIZE the number of extra_cmds generated. Straightforward environments do not need any.
- Do NOT expose any ports.
- Do NOT generate any CMD or ENTRYPOINT extra commands.
`,
		}},
	}
	if len(initFiles) > 0 {
		msg.Content[0].Text += "Here is the content of several files from the repository that may be relevant:\n\n"
	}

	for _, name := range slices.Sorted(maps.Keys(initFiles)) {
		msg.Content = append(msg.Content, llm.StringContent(fmt.Sprintf("Here is the contents %s:\n<file>\n%s\n</file>\n\n", name, initFiles[name])))
	}
	msg.Content = append(msg.Content, llm.StringContent("Now call the dockerfile tool."))
	res, err := convo.SendMessage(msg)
	if err != nil {
		return "", err
	}
	if res.StopReason != llm.StopReasonToolUse {
		return "", fmt.Errorf("expected stop reason %q, got %q", llm.StopReasonToolUse, res.StopReason)
	}
	if _, err := convo.ToolResultContents(context.TODO(), res); err != nil {
		return "", err
	}
	if !toolCalled {
		return "", fmt.Errorf("no dockerfile returned")
	}

	tmpl := dockerfileDefaultTmpl
	if tag := dockerfileBaseHash(); checkTagExists(tag) != nil {
		// In development, if you edit dockerfileBase but don't release
		// (as is reasonable for testing things!) the hash won't exist
		// yet. In that case, we skip the sketch image and build it ourselves.
		fmt.Printf("published container tag %s:%s missing; building locally\n", dockerImgName, tag)
		tmpl = dockerfileBase + dockerfileFragment
	}
	buf := new(bytes.Buffer)
	err = template.Must(template.New("dockerfile").Parse(tmpl)).Execute(buf, map[string]string{
		"ExtraCmds":     dockerfileExtraCmds,
		"SubDir":        subPathWorkingDir,
		"InitFilesHash": hashInitFiles(initFiles),
	})
	if err != nil {
		return "", fmt.Errorf("dockerfile template failed: %w", err)
	}

	return buf.String(), nil
}

// For future reference: we can find the current git branch/checkout with: git symbolic-ref -q --short HEAD || git describe --tags --exact-match 2>/dev/null || git rev-parse HEAD

func readInitFiles(fsys fs.FS) (map[string]string, error) {
	result := make(map[string]string)

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return fs.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}

		// Case 1: Check for README files
		// TODO: find README files between the .git root (where we start)
		// and the dir that sketch was initialized. This needs more info
		// plumbed to this function.
		if strings.HasPrefix(strings.ToLower(path), "readme") {
			content, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			result[path] = string(content)
			return nil
		}

		// Case 2: Check for GitHub workflow files
		if strings.HasPrefix(path, ".github/workflows/") {
			content, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			result[path] = string(content)
			return nil
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
