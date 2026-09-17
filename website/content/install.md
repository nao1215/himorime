---
title: Installation
description: Install yahiko with go install or from GitHub Releases, and verify the checksums, signature, SBOM and provenance of a release.
toc: true
---

yahiko is a single binary with no runtime dependencies. `git` is needed for
`yahiko compare` and `yahiko ci`; `yahiko run` does not use it.

## go install

```console
$ go install github.com/nao1215/yahiko@latest
```

This needs Go 1.26 or later and installs into `$(go env GOPATH)/bin`.

## GitHub Releases

Every release on [GitHub Releases](https://github.com/nao1215/yahiko/releases)
carries archives for Linux, macOS and Windows on amd64 and arm64, `.deb`,
`.rpm` and `.apk` packages for Linux, shell completions, checksums, SBOMs,
a signature and build provenance.

```console
$ VERSION=1.0.0
$ curl -fsSLO "https://github.com/nao1215/yahiko/releases/download/v${VERSION}/yahiko_${VERSION}_linux_amd64.tar.gz"
$ tar -xzf "yahiko_${VERSION}_linux_amd64.tar.gz" yahiko
$ ./yahiko version
```

Windows archives are `.zip` files and contain `yahiko.exe`. The archives also
hold completion scripts under `completions/`; the Linux packages install them
into the standard bash, zsh and fish locations.

## Shell completion

```console
$ yahiko completion bash > ~/.local/share/bash-completion/completions/yahiko
$ yahiko completion zsh > "${fpath[1]}/_yahiko"
$ yahiko completion fish > ~/.config/fish/completions/yahiko.fish
```

In PowerShell, add `yahiko completion powershell | Out-String | Invoke-Expression`
to your profile.

## Verify a release

Every release is built by GoReleaser in GitHub Actions from the tag.
`checksums.txt` lists the SHA-256 of every archive and package. It is signed
with [cosign](https://github.com/sigstore/cosign) keyless signing, so the
signature proves it was produced by the release workflow of this repository.

1. Check the file you downloaded against the checksums:

   ```console
   $ curl -fsSLO "https://github.com/nao1215/yahiko/releases/download/v${VERSION}/checksums.txt"
   $ sha256sum --ignore-missing -c checksums.txt
   ```

2. Verify the signature of `checksums.txt`:

   ```console
   $ curl -fsSLO "https://github.com/nao1215/yahiko/releases/download/v${VERSION}/checksums.txt.sigstore.json"
   $ cosign verify-blob \
       --bundle checksums.txt.sigstore.json \
       --certificate-identity "https://github.com/nao1215/yahiko/.github/workflows/release.yml@refs/tags/v${VERSION}" \
       --certificate-oidc-issuer https://token.actions.githubusercontent.com \
       checksums.txt
   ```

3. Verify the build provenance (SLSA, issued through GitHub's OIDC token):

   ```console
   $ gh attestation verify "yahiko_${VERSION}_linux_amd64.tar.gz" --repo nao1215/yahiko
   ```

Each archive has an SPDX SBOM next to it, `<archive>.sbom.json`, listing the
Go modules compiled into the binary.
