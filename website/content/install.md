---
title: Installation
description: Install himorime with go install, Homebrew, GitHub Releases or the setup-himorime GitHub Action, and verify the checksums, signature, SBOM and provenance of a release.
toc: true
---

himorime is a single binary with no runtime dependencies. `git` is needed for `himorime compare` and `himorime ci`; `himorime run` uses it only to record the commit in the report, and works without it.

## go install

```console
$ go install github.com/nao1215/himorime@latest
```

This needs Go 1.26 or later and installs into `$(go env GOPATH)/bin`.

## Homebrew

On macOS:

```console
$ brew install --cask nao1215/tap/himorime
```

The cask installs the shell completions for bash, zsh and fish.

## GitHub Actions

[setup-himorime](https://github.com/nao1215/setup-himorime) installs a prebuilt release on Linux, macOS and Windows runners and verifies it against `checksums.txt`:

```yaml
- uses: nao1215/setup-himorime@v0
- run: himorime ci
```

See [GitHub Actions](/github-actions/) for a complete workflow.

## GitHub Releases

Every release on [GitHub Releases](https://github.com/nao1215/himorime/releases) carries archives for Linux, macOS and Windows on amd64 and arm64, `.deb`, `.rpm` and `.apk` packages for Linux, shell completions, checksums, SBOMs, a signature and build provenance.

```console
$ VERSION=0.1.0
$ curl -fsSLO "https://github.com/nao1215/himorime/releases/download/v${VERSION}/himorime_${VERSION}_linux_amd64.tar.gz"
$ tar -xzf "himorime_${VERSION}_linux_amd64.tar.gz" himorime
$ ./himorime version
```

Windows archives are `.zip` files and contain `himorime.exe`. The archives also hold completion scripts under `completions/`; the Linux packages install them into the standard bash, zsh and fish locations.

## Shell completion

```console
$ himorime completion bash > ~/.local/share/bash-completion/completions/himorime
$ himorime completion zsh > "${fpath[1]}/_himorime"
$ himorime completion fish > ~/.config/fish/completions/himorime.fish
```

In PowerShell, add `himorime completion powershell | Out-String | Invoke-Expression` to your profile.

## Verify a release

Every release is built by GoReleaser in GitHub Actions from the tag. `checksums.txt` lists the SHA-256 of every archive and package. It is signed with [cosign](https://github.com/sigstore/cosign) keyless signing, so the signature proves it was produced by the release workflow of this repository.

1. Check the file you downloaded against the checksums:

   ```console
   $ curl -fsSLO "https://github.com/nao1215/himorime/releases/download/v${VERSION}/checksums.txt"
   $ sha256sum --ignore-missing -c checksums.txt
   ```

2. Verify the signature of `checksums.txt`:

   ```console
   $ curl -fsSLO "https://github.com/nao1215/himorime/releases/download/v${VERSION}/checksums.txt.sigstore.json"
   $ cosign verify-blob \
       --bundle checksums.txt.sigstore.json \
       --certificate-identity "https://github.com/nao1215/himorime/.github/workflows/release.yml@refs/tags/v${VERSION}" \
       --certificate-oidc-issuer https://token.actions.githubusercontent.com \
       checksums.txt
   ```

3. Verify the build provenance (SLSA, issued through GitHub's OIDC token):

   ```console
   $ gh attestation verify "himorime_${VERSION}_linux_amd64.tar.gz" --repo nao1215/himorime
   ```

Each archive has an SPDX SBOM next to it, `<archive>.sbom.json`, listing the Go modules compiled into the binary.
