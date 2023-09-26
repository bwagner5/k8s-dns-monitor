# k8s-dns-monitor

k8s-dns-monitor provides a scaffold for a go CLI. It currently utilizes cobra and takes opinions on what github workflows exist and do as well as the Makefile including dev tooling like golangci and goreleaser.

You can run the following commands to replace occurrences of `k8s-dns-monitor` with whatever your CLI is called:

```
find . -path ./.git -prune -o -print -exec sed -E -i.bak 's/k8s-dns-monitor/<FILL IN CLI NAME>/g' {} \;
find . -name "*.bak" -type f -delete
```

**NOTE:** 
goreleaser requires a personal access token to publish a homebrew formula to a tap in another repo since github action token are only valid for the repo it's running in. The personal access token should be named `MY_GITHUB_TOKEN` and have `repo` permissions.

Below is an example starting point for a README.

# k8s-dns-monitor

DESCRIPTION HERE

## Usage:


```
Put Usage here
Usage:
  k8s-dns-monitor [command]
...
```

## Installation:

```
brew install bwagner5/wagner/k8s-dns-monitor
```

Packages, binaries, and archives are published for all major platforms (Mac amd64/arm64 & Linux amd64/arm64):

Debian / Ubuntu:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
OS=`uname | tr '[:upper:]' '[:lower:]'`
wget https://github.com/bwagner5/k8s-dns-monitor/releases/download/v0.0.1/k8s-dns-monitor_0.0.1_${OS}_${ARCH}.deb
dpkg --install k8s-dns-monitor_0.0.2_linux_amd64.deb
k8s-dns-monitor --help
```

RedHat:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
OS=`uname | tr '[:upper:]' '[:lower:]'`
rpm -i https://github.com/bwagner5/k8s-dns-monitor/releases/download/v0.0.1/k8s-dns-monitor_0.0.1_${OS}_${ARCH}.rpm
```

Download Binary Directly:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
OS=`uname | tr '[:upper:]' '[:lower:]'`
wget -qO- https://github.com/bwagner5/k8s-dns-monitor/releases/download/v0.0.1/k8s-dns-monitor_0.0.1_${OS}_${ARCH}.tar.gz | tar xvz
chmod +x k8s-dns-monitor
```

## Examples: 

EXAMPLES HERE