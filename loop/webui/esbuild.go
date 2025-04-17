// Package webui provides the web interface for the sketch loop.
// It bundles typescript files into JavaScript using esbuild.
//
// This is substantially the same mechanism as /esbuild.go in this repo as well.
package webui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	esbuildcli "github.com/evanw/esbuild/pkg/cli"
)

//go:generate npm ci

// We only want to embed `node_modules`, but we instead as for `*` because
// if it does not exist it means the developer has not run npm install yet.
// We do not want a compile time error in that case right now.

//go:embed *
var embedded embed.FS

func embeddedHash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		f, err := embedded.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("embedded hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil))[:32], nil
}

func cleanBuildDir(buildDir string) error {
	err := fs.WalkDir(os.DirFS(buildDir), ".", func(path string, d fs.DirEntry, err error) error {
		if d.Name() == "." {
			return nil
		}
		if d.Name() == "node_modules" {
			return fs.SkipDir
		}
		osPath := filepath.Join(buildDir, path)
		os.RemoveAll(osPath)
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("clean build dir: %w", err)
	}
	return nil
}

func unpackFS(out string, srcFS fs.FS) error {
	err := fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if d.Name() == "." {
			return nil
		}
		if d.IsDir() {
			if err := os.Mkdir(filepath.Join(out, path), 0o777); err != nil {
				return err
			}
			return nil
		}
		f, err := srcFS.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		dst, err := os.Create(filepath.Join(out, path))
		if err != nil {
			return err
		}
		defer dst.Close()
		if _, err := io.Copy(dst, f); err != nil {
			return err
		}
		if err := dst.Close(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("unpack fs into out dir %s: %w", out, err)
	}
	return nil
}

func ZipPath() (string, error) {
	_, hashZip, err := zipPath()
	return hashZip, err
}

func zipPath() (cacheDir, hashZip string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	hash, err := embeddedHash()
	if err != nil {
		return "", "", err
	}
	cacheDir = filepath.Join(homeDir, ".cache", "sketch", "webui")
	return cacheDir, filepath.Join(cacheDir, "skui-"+hash+".zip"), nil
}

// Build unpacks and esbuild's all bundleTs typescript files
func Build() (fs.FS, error) {
	cacheDir, hashZip, err := zipPath()
	if err != nil {
		return nil, err
	}
	buildDir := filepath.Join(cacheDir, "build")
	if err := os.MkdirAll(buildDir, 0o777); err != nil { // make sure .cache/sketch/build exists
		return nil, err
	}
	if b, err := os.ReadFile(hashZip); err == nil {
		// Build already done, serve it out.
		return zip.NewReader(bytes.NewReader(b), int64(len(b)))
	}

	// TODO: try downloading "https://sketch.dev/webui/"+filepath.Base(hashZip)

	// We need to do a build.

	// Clear everything out of the build directory except node_modules.
	if err := cleanBuildDir(buildDir); err != nil {
		return nil, err
	}
	bundleTs := []string{"src/web-components/sketch-app-shell.ts"}
	for _, tsName := range bundleTs {
		if err := esbuildBundle(jsDir, filepath.Join(tmpDir, tsName), ""); err != nil {
			return nil, fmt.Errorf("esbuild: %s: %w", tsName, err)
		}
	}

	res := &memFS{
		m: make(map[string][]byte),
	}
	// Copy all esbuild outputs into res
	err = fs.WalkDir(os.DirFS(jsDir), ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(filepath.Join(jsDir, path))
		if err != nil {
			return err
		}
		res.m[path] = b
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Copy all HTML, CSS, and JS files from src as well
	err = fs.WalkDir(embedded, "src", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") {
			b, err := embedded.ReadFile(path)
			if err != nil {
				return err
			}
			// Strip the "src" prefix from HTML and CSS files
			destPath := strings.TrimPrefix(path, "src/")
			res.m[destPath] = b
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Copy xterm.css from node_modules
	xtermCssPath := "node_modules/@xterm/xterm/css/xterm.css"
	xtermCss, err := embedded.ReadFile(xtermCssPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read xterm.css: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpHashDir, "xterm.css"), xtermCss, 0o666); err != nil {
		return nil, fmt.Errorf("failed to write xterm.css: %w", err)
	}

	// Everything succeeded, so we write tmpHashDir to hashZip
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	if err := w.AddFS(os.DirFS(tmpHashDir)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(hashZip, buf.Bytes(), 0o666); err != nil {
		return nil, err
	}
	return zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
}

func esbuildBundle(outDir, src string, metafilePath string) error {
	args := []string{
		src,
		"--bundle",
		"--sourcemap",
		"--log-level=error",
		// Disable minification for now
		// "--minify",
		"--outdir=" + outDir,
	}

	// Add metafile option if path is provided
	if metafilePath != "" {
		args = append(args, "--metafile="+metafilePath)
	}

	ret := esbuildcli.Run(args)
	if ret != 0 {
		return fmt.Errorf("esbuild %s failed: %d", filepath.Base(src), ret)
	}
	return nil
}

// GenerateBundleMetafile creates metafiles for bundle analysis with esbuild.
//
// The metafiles contain information about bundle size and dependencies
// that can be visualized at https://esbuild.github.io/analyze/
//
// It takes the output directory where the metafiles will be written.
// Returns the file path of the generated metafiles.
func GenerateBundleMetafile(outputDir string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "bundle-analysis-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", err
	}

	// Ensure we have a source to bundle
	if err := unpackTS(tmpDir, embedded); err != nil {
		return "", err
	}

	// All bundles to analyze
	bundleTs := []string{"src/web-components/sketch-app-shell.ts"}
	metafiles := make([]string, len(bundleTs))

	for i, tsName := range bundleTs {
		// Create a metafile path for this bundle
		baseFileName := filepath.Base(tsName)
		metaFileName := strings.TrimSuffix(baseFileName, ".ts") + ".meta.json"
		metafilePath := filepath.Join(outputDir, metaFileName)
		metafiles[i] = metafilePath

		// Bundle with metafile generation
		outTmpDir, err := os.MkdirTemp("", "metafile-bundle-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(outTmpDir)

		if err := esbuildBundle(outTmpDir, filepath.Join(tmpDir, tsName), metafilePath); err != nil {
			return "", fmt.Errorf("failed to generate metafile for %s: %w", tsName, err)
		}
	}

	return outputDir, nil
}
