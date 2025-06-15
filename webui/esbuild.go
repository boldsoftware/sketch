// Package webui provides the web interface for the sketch loop.
// It bundles typescript files into JavaScript using esbuild.
package webui

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	esbuildcli "github.com/evanw/esbuild/pkg/cli"
)

//go:embed package.json package-lock.json src tsconfig.json
var embedded embed.FS

//go:generate go run ../cmd/go2ts -o src/types.ts

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

// copyMonacoAssets copies Monaco editor assets to the output directory
func copyMonacoAssets(buildDir, outDir string) error {
	// Create Monaco directories
	monacoEditorDir := filepath.Join(outDir, "monaco", "min", "vs", "editor")
	codiconDir := filepath.Join(outDir, "monaco", "min", "vs", "base", "browser", "ui", "codicons", "codicon")

	if err := os.MkdirAll(monacoEditorDir, 0o777); err != nil {
		return fmt.Errorf("failed to create monaco editor directory: %w", err)
	}

	if err := os.MkdirAll(codiconDir, 0o777); err != nil {
		return fmt.Errorf("failed to create codicon directory: %w", err)
	}

	// Copy Monaco editor CSS
	editorCssPath := "node_modules/monaco-editor/min/vs/editor/editor.main.css"
	editorCss, err := os.ReadFile(filepath.Join(buildDir, editorCssPath))
	if err != nil {
		return fmt.Errorf("failed to read monaco editor CSS: %w", err)
	}

	if err := os.WriteFile(filepath.Join(monacoEditorDir, "editor.main.css"), editorCss, 0o666); err != nil {
		return fmt.Errorf("failed to write monaco editor CSS: %w", err)
	}

	// Copy Codicon font
	codiconFontPath := "node_modules/monaco-editor/min/vs/base/browser/ui/codicons/codicon/codicon.ttf"
	codiconFont, err := os.ReadFile(filepath.Join(buildDir, codiconFontPath))
	if err != nil {
		return fmt.Errorf("failed to read codicon font: %w", err)
	}

	if err := os.WriteFile(filepath.Join(codiconDir, "codicon.ttf"), codiconFont, 0o666); err != nil {
		return fmt.Errorf("failed to write codicon font: %w", err)
	}

	return nil
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
	tmpHashDir := filepath.Join(buildDir, "out")
	if err := os.Mkdir(tmpHashDir, 0o777); err != nil {
		return nil, err
	}

	// Unpack everything from embedded into build dir.
	if err := unpackFS(buildDir, embedded); err != nil {
		return nil, err
	}

	// Do the build. Don't install dev dependencies, because they can be large
	// and slow enough to install that the /init requests from the host process
	// will run out of retries and the whole thing exits. We do need better health
	// checking in general, but that's a separate issue. Don't do slow stuff here:
	cmd := exec.Command("npm", "ci", "--omit", "dev")
	cmd.Dir = buildDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("npm ci: %s: %v", out, err)
	}
	// Create all bundles
	bundleTs := []string{
		"src/web-components/sketch-app-shell.ts",
		"src/web-components/mobile-app-shell.ts",
		"src/web-components/sketch-monaco-view.ts",
		"node_modules/monaco-editor/esm/vs/editor/editor.worker.js",
		"node_modules/monaco-editor/esm/vs/language/typescript/ts.worker.js",
		"node_modules/monaco-editor/esm/vs/language/html/html.worker.js",
		"node_modules/monaco-editor/esm/vs/language/css/css.worker.js",
		"node_modules/monaco-editor/esm/vs/language/json/json.worker.js",
	}

	// Additionally create a standalone Monaco bundle for caching
	monacoHash, err := createStandaloneMonacoBundle(tmpHashDir, buildDir)
	if err != nil {
		return nil, fmt.Errorf("create monaco bundle: %w", err)
	}
	_ = monacoHash // We created it for caching benefits, but don't use it yet

	// Bundle all files normally
	for _, tsName := range bundleTs {
		if err := esbuildBundle(tmpHashDir, filepath.Join(buildDir, tsName), ""); err != nil {
			return nil, fmt.Errorf("esbuild: %s: %w", tsName, err)
		}
	}

	// Copy Monaco editor assets
	if err := copyMonacoAssets(buildDir, tmpHashDir); err != nil {
		return nil, fmt.Errorf("failed to copy Monaco assets: %w", err)
	}

	// Copy src files used directly into the new hash output dir.
	err = fs.WalkDir(embedded, "src", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			if path == "src/web-components/demo" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, "mockServiceWorker.js") {
			return nil
		}
		if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") {
			b, err := embedded.ReadFile(path)
			if err != nil {
				return err
			}
			dstPath := filepath.Join(tmpHashDir, strings.TrimPrefix(path, "src/"))
			if err := os.WriteFile(dstPath, b, 0o777); err != nil {
				return err
			}
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Copy xterm.css from node_modules
	const xtermCssPath = "node_modules/@xterm/xterm/css/xterm.css"
	xtermCss, err := os.ReadFile(filepath.Join(buildDir, xtermCssPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read xterm.css: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpHashDir, "xterm.css"), xtermCss, 0o666); err != nil {
		return nil, fmt.Errorf("failed to write xterm.css: %w", err)
	}

	// Compress all .js, .js.map, and .css files with gzip, leaving the originals in place
	err = filepath.Walk(tmpHashDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Check if file is a .js or .js.map file
		if !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".js.map") && !strings.HasSuffix(path, ".css") {
			return nil
		}

		// Read the original file
		origData, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		// Create a gzipped file
		gzipPath := path + ".gz"
		gzipFile, err := os.Create(gzipPath)
		if err != nil {
			return fmt.Errorf("failed to create gzip file %s: %w", gzipPath, err)
		}
		defer gzipFile.Close()

		// Create a gzip writer
		gzWriter := gzip.NewWriter(gzipFile)
		defer gzWriter.Close()

		// Write the original file content to the gzip writer
		_, err = gzWriter.Write(origData)
		if err != nil {
			return fmt.Errorf("failed to write to gzip file %s: %w", gzipPath, err)
		}

		// Ensure we flush and close properly
		if err := gzWriter.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer for %s: %w", gzipPath, err)
		}
		if err := gzipFile.Close(); err != nil {
			return fmt.Errorf("failed to close gzip file %s: %w", gzipPath, err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compress .js/.js.map/.css files: %w", err)
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

func esbuildBundle(outDir, src, metafilePath string) error {
	args := []string{
		src,
		"--bundle",
		"--sourcemap",
		"--log-level=error",
		"--minify",
		"--outdir=" + outDir,
		"--loader:.ttf=file",
		"--loader:.eot=file",
		"--loader:.woff=file",
		"--loader:.woff2=file",
		// This changes where the sourcemap points to; we need relative dirs if we're proxied into a subdirectory.
		"--public-path=.",
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

// unpackTS unpacks all the typescript-relevant files from the embedded filesystem into tmpDir.
func unpackTS(outDir string, embedded fs.FS) error {
	return fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		tgt := filepath.Join(outDir, path)
		if d.IsDir() {
			if err := os.MkdirAll(tgt, 0o777); err != nil {
				return err
			}
			return nil
		}
		if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".css") {
			return nil
		}
		data, err := fs.ReadFile(embedded, path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(tgt, data, 0o666); err != nil {
			return err
		}
		return nil
	})
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
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}

	cacheDir, _, err := zipPath()
	if err != nil {
		return "", err
	}
	buildDir := filepath.Join(cacheDir, "build")
	if err := os.MkdirAll(buildDir, 0o777); err != nil { // make sure .cache/sketch/build exists
		return "", err
	}

	// Ensure we have a source to bundle
	if err := unpackTS(tmpDir, embedded); err != nil {
		return "", err
	}

	// All bundles to analyze
	bundleTs := []string{
		"src/web-components/sketch-app-shell.ts",
		"src/web-components/mobile-app-shell.ts",
		"src/web-components/sketch-monaco-view.ts",
	}
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

		if err := esbuildBundle(outTmpDir, filepath.Join(buildDir, tsName), metafilePath); err != nil {
			return "", fmt.Errorf("failed to generate metafile for %s: %w", tsName, err)
		}
	}

	return outputDir, nil
}

// createStandaloneMonacoBundle creates a separate Monaco editor bundle with content-based hash
// This is useful for caching Monaco separately from the main application bundles
func createStandaloneMonacoBundle(outDir, buildDir string) (string, error) {
	// Create a temporary entry file that imports Monaco
	monacoEntryContent := `import * as monaco from 'monaco-editor';
export default monaco;
`
	monacoEntryPath := filepath.Join(buildDir, "monaco-standalone-entry.js")
	if err := os.WriteFile(monacoEntryPath, []byte(monacoEntryContent), 0o666); err != nil {
		return "", fmt.Errorf("write monaco entry: %w", err)
	}

	// Calculate hash of monaco-editor package for content-based naming
	monacoPackageJson := filepath.Join(buildDir, "node_modules", "monaco-editor", "package.json")
	monacoContent, err := os.ReadFile(monacoPackageJson)
	if err != nil {
		return "", fmt.Errorf("read monaco package.json: %w", err)
	}

	h := sha256.New()
	h.Write(monacoContent)
	monacoHash := hex.EncodeToString(h.Sum(nil))[:16]

	// Bundle Monaco with content-based filename
	monacoOutputName := fmt.Sprintf("monaco-standalone-%s.js", monacoHash)
	monacoOutputPath := filepath.Join(outDir, monacoOutputName)

	args := []string{
		monacoEntryPath,
		"--bundle",
		"--sourcemap",
		"--minify",
		"--log-level=error",
		"--outfile=" + monacoOutputPath,
		"--format=esm",
		"--loader:.ttf=file",
		"--loader:.eot=file",
		"--loader:.woff=file",
		"--loader:.woff2=file",
		"--public-path=.",
	}

	ret := esbuildcli.Run(args)
	if ret != 0 {
		return "", fmt.Errorf("esbuild monaco bundle failed: %d", ret)
	}

	return monacoHash, nil
}
