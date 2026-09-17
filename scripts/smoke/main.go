// Command smoke checks the artifacts GoReleaser wrote to a dist directory
// before they are published, on any operating system:
//
//	go run ./scripts/smoke [-host-only] dist
//
// It verifies that every supported platform has an archive holding the binary,
// the license, the documentation and the completion scripts; that the Linux
// packages, SBOMs and checksums exist and agree; that the Homebrew cask names
// the archives' checksums; that every binary was built for the platform its
// archive names, without cgo and with the version injected; and it runs the
// binary built for this machine. With -host-only, only the archive for this
// machine is checked and run, which is what the macOS and Windows jobs do
// with archives built on Linux.
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var platforms = []struct{ goos, goarch string }{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

func main() {
	hostOnly := flag.Bool("host-only", false, "check and run only the archive for this machine")
	flag.Parse()
	dist := "dist"
	if flag.NArg() > 0 {
		dist = flag.Arg(0)
	}
	var failures []string
	fail := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf(format, args...))
	}
	version, err := readVersion(dist)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smoke:", err)
		os.Exit(1)
	}
	fmt.Printf("smoke: checking yahiko %s in %s\n", version, dist)

	for _, p := range platforms {
		host := p.goos == runtime.GOOS && p.goarch == runtime.GOARCH
		if *hostOnly && !host {
			continue
		}
		checkArchive(dist, version, p.goos, p.goarch, host, fail)
	}
	if !*hostOnly {
		checkPackages(dist, version, fail)
		checkChecksums(dist, version, fail)
		checkCask(dist, version, fail)
	}

	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, "FAIL:", f)
		}
		os.Exit(1)
	}
	fmt.Println("smoke: all checks passed")
}

func readVersion(dist string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dist, "metadata.json"))
	if err != nil {
		// A per-OS job receives only archives; derive the version from them.
		matches, _ := filepath.Glob(filepath.Join(dist, "yahiko_*_linux_amd64.tar.gz"))
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(dist, "yahiko_*_*_*.*"))
		}
		if len(matches) == 0 {
			return "", fmt.Errorf("no metadata.json and no archive in %s", dist)
		}
		base := filepath.Base(matches[0])
		parts := strings.Split(strings.TrimPrefix(base, "yahiko_"), "_")
		return parts[0], nil
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", err
	}
	return meta.Version, nil
}

func checkArchive(dist, version, goos, goarch string, host bool, fail func(string, ...any)) {
	exe := ""
	ext := ".tar.gz"
	if goos == "windows" {
		exe, ext = ".exe", ".zip"
	}
	name := fmt.Sprintf("yahiko_%s_%s_%s%s", version, goos, goarch, ext)
	path := filepath.Join(dist, name)
	files, err := extract(path)
	if err != nil {
		fail("%s: %v", name, err)
		return
	}
	for _, want := range []string{"yahiko" + exe, "LICENSE", "README.md", "CHANGELOG.md", "completions/yahiko.bash", "completions/yahiko.zsh", "completions/yahiko.fish", "completions/yahiko.ps1"} {
		if len(files[want]) == 0 {
			fail("%s lacks %s", name, want)
		}
	}
	bin := files["yahiko"+exe]
	if len(bin) == 0 {
		return
	}
	tmp, err := os.MkdirTemp("", "yahiko-smoke-")
	if err != nil {
		fail("%v", err)
		return
	}
	defer os.RemoveAll(tmp)
	binPath := filepath.Join(tmp, "yahiko"+exe)
	if err := os.WriteFile(binPath, bin, 0o700); err != nil {
		fail("%v", err)
		return
	}
	checkBuildInfo(name, binPath, goos, goarch, version, fail)
	if !strings.Contains(string(files["completions/yahiko.bash"]), "complete -o default -F _yahiko yahiko") {
		fail("%s: the bash completion is not the generated script", name)
	}
	if !host {
		return
	}
	runBinary(name, binPath, tmp, version, fail)
}

func checkBuildInfo(name, bin, goos, goarch, version string, fail func(string, ...any)) {
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		fail("%s: read build info: %v", name, err)
		return
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != goos || settings["GOARCH"] != goarch {
		fail("%s: binary is %s/%s", name, settings["GOOS"], settings["GOARCH"])
	}
	if settings["CGO_ENABLED"] != "0" {
		fail("%s: built with CGO_ENABLED=%s", name, settings["CGO_ENABLED"])
	}
	if settings["-trimpath"] != "true" {
		fail("%s: not built with -trimpath", name)
	}
	// With -trimpath the toolchain does not record -ldflags, so the injected
	// version is checked by looking for it in the binary itself; the host
	// binary is also asked for it by runBinary.
	data, err := os.ReadFile(bin)
	if err != nil || !bytes.Contains(data, []byte("v"+version)) {
		fail("%s: version v%s was not injected", name, version)
	}
}

func runBinary(name, bin, dir, version string, fail func(string, ...any)) {
	out, err := command(dir, bin, "version")
	if err != nil || !strings.HasPrefix(out, "yahiko v"+version+" (") {
		fail("%s: yahiko version = %q, %v", name, out, err)
	}
	if out, err := command(dir, bin, "init"); err != nil {
		fail("%s: yahiko init: %v\n%s", name, err, out)
	}
	if out, err := command(dir, bin, "validate"); err != nil || !strings.Contains(out, "ok (1 benchmark") {
		fail("%s: yahiko validate: %v\n%s", name, err, out)
	}
	if out, err := command(dir, bin, "run", "--runs", "2", "--warmup", "0", "--quiet", "--format", "json"); err != nil || !strings.Contains(out, `"schema_version": "1"`) {
		fail("%s: yahiko run: %v\n%s", name, err, out)
	}
	if _, err := command(dir, bin, "frobnicate"); !exitCode(err, 3) {
		fail("%s: an unknown command must exit 3, got %v", name, err)
	}
	fmt.Printf("smoke: ran %s\n", name)
}

func command(dir, bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func exitCode(err error, want int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == want
}

func extract(path string) (map[string][]byte, error) {
	files := map[string][]byte{}
	if strings.HasSuffix(path, ".zip") {
		r, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		for _, f := range r.File {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(rc, 64<<20))
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			files[filepath.ToSlash(f.Name)] = data
		}
		return files, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
		if err != nil {
			return nil, err
		}
		files[h.Name] = data
	}
}

func checkPackages(dist, version string, fail func(string, ...any)) {
	magic := map[string][]byte{"deb": []byte("!<arch>\n"), "rpm": {0xed, 0xab, 0xee, 0xdb}, "apk": {0x1f, 0x8b}}
	for _, arch := range []string{"amd64", "arm64"} {
		for ext, m := range magic {
			name := fmt.Sprintf("yahiko_%s_linux_%s.%s", version, arch, ext)
			data, err := os.ReadFile(filepath.Join(dist, name))
			if err != nil {
				fail("%s: %v", name, err)
				continue
			}
			if !bytes.HasPrefix(data, m) {
				fail("%s is not a %s package", name, ext)
			}
		}
		deb := filepath.Join(dist, fmt.Sprintf("yahiko_%s_linux_%s.deb", version, arch))
		if dpkg, err := exec.LookPath("dpkg-deb"); err == nil {
			out, err := exec.Command(dpkg, "-c", deb).Output()
			if err != nil {
				fail("dpkg-deb -c %s: %v", deb, err)
				continue
			}
			for _, want := range []string{"./usr/bin/yahiko", "./usr/share/bash-completion/completions/yahiko", "./usr/share/zsh/vendor-completions/_yahiko", "./usr/share/fish/vendor_completions.d/yahiko.fish"} {
				if !bytes.Contains(out, []byte(want+"\n")) {
					fail("%s lacks %s", filepath.Base(deb), want)
				}
			}
		}
	}
	if os.Getenv("SMOKE_SKIP_SBOM") == "1" {
		return
	}
	sboms, _ := filepath.Glob(filepath.Join(dist, "*.sbom.json"))
	if len(sboms) != len(platforms) {
		fail("found %d SBOMs, want one per archive (%d)", len(sboms), len(platforms))
	}
	for _, s := range sboms {
		var doc struct {
			SPDXVersion string `json:"spdxVersion"`
		}
		data, err := os.ReadFile(s)
		if err != nil || json.Unmarshal(data, &doc) != nil || !strings.HasPrefix(doc.SPDXVersion, "SPDX-") {
			fail("%s is not an SPDX document", filepath.Base(s))
		}
	}
}

func checkChecksums(dist, version string, fail func(string, ...any)) {
	f, err := os.Open(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		fail("checksums.txt: %v", err)
		return
	}
	defer f.Close()
	listed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			fail("malformed checksum line %q", sc.Text())
			continue
		}
		listed[fields[1]] = true
		data, err := os.ReadFile(filepath.Join(dist, fields[1]))
		if err != nil {
			fail("checksums.txt lists %s: %v", fields[1], err)
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != fields[0] {
			fail("checksum mismatch for %s", fields[1])
		}
	}
	for _, p := range platforms {
		ext := ".tar.gz"
		if p.goos == "windows" {
			ext = ".zip"
		}
		name := fmt.Sprintf("yahiko_%s_%s_%s%s", version, p.goos, p.goarch, ext)
		if !listed[name] {
			fail("checksums.txt does not list %s", name)
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, ext := range []string{"deb", "rpm", "apk"} {
			if name := fmt.Sprintf("yahiko_%s_linux_%s.%s", version, arch, ext); !listed[name] {
				fail("checksums.txt does not list %s", name)
			}
		}
	}
}

func checkCask(dist, version string, fail func(string, ...any)) {
	data, err := os.ReadFile(filepath.Join(dist, "homebrew", "Casks", "yahiko.rb"))
	if err != nil {
		fail("homebrew cask: %v", err)
		return
	}
	cask := string(data)
	for _, want := range []string{`binary "yahiko"`, `version "` + version + `"`, "completions/yahiko.bash"} {
		if !strings.Contains(cask, want) {
			fail("the cask lacks %s", want)
		}
	}
	for _, p := range platforms {
		if p.goos == "windows" {
			continue
		}
		archive := fmt.Sprintf("yahiko_%s_%s_%s.tar.gz", version, p.goos, p.goarch)
		body, err := os.ReadFile(filepath.Join(dist, archive))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		if !strings.Contains(cask, hex.EncodeToString(sum[:])) {
			fail("the cask lacks the checksum of %s", archive)
		}
	}
}
