#!/bin/sh
# Installs clawsh on Linux or macOS from its GitHub release:
#
#   curl -fsSL https://varogonz95.github.io/clawsh/install.sh | sh
#
# Downloads the archive for this machine, checks it against the release's
# checksums.txt, and puts the binary in ~/.local/bin (no sudo). Settings:
#   CLAWSH_VERSION=0.1.0           a specific release instead of the latest
#   CLAWSH_INSTALL_DIR=/some/bin   where the binary goes
# Pass them to sh, not curl:  curl -fsSL .../install.sh | CLAWSH_VERSION=0.1.0 sh
#
# Everything runs from main at the bottom, so a download cut off halfway
# does nothing instead of running half a script.

set -eu

RELEASES="${CLAWSH_RELEASES_URL:-https://github.com/varogonz95/clawsh/releases}"

say() { printf 'clawsh: %s\n' "$*" >&2; }
die() { say "$*"; exit 1; }

# fetch URL FILE, or fetch URL to stdout
fetch() {
	if command -v curl >/dev/null 2>&1; then
		if [ $# -eq 2 ]; then curl -fsSL -o "$2" "$1"; else curl -fsSL "$1"; fi
	elif command -v wget >/dev/null 2>&1; then
		if [ $# -eq 2 ]; then wget -qO "$2" "$1"; else wget -qO- "$1"; fi
	else
		die "needs curl or wget"
	fi
}

# The tag the releases/latest page redirects to, e.g. v0.1.0.
latest_tag() {
	if command -v curl >/dev/null 2>&1; then
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES/latest")
	else
		# wget prints "Location: <url> [following]" for each redirect
		url=$(wget -S --spider "$RELEASES/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *\([^ ]*\).*/\1/p' | tail -n 1)
	fi
	tag=${url##*/}
	tag=$(printf '%s' "$tag" | tr -d '\r')
	case $tag in
		v[0-9]*) printf '%s\n' "$tag" ;;
		*) die "couldn't find the latest release at $RELEASES/latest" ;;
	esac
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		die "needs sha256sum or shasum to check the download"
	fi
}

main() {
	case $(uname -s) in
		Linux) os=linux ;;
		Darwin) os=darwin ;;
		*) die "unsupported OS $(uname -s); on Windows use install.ps1, otherwise build from source: https://github.com/varogonz95/clawsh" ;;
	esac
	case $(uname -m) in
		x86_64 | amd64) arch=amd64 ;;
		aarch64 | arm64) arch=arm64 ;;
		*) die "no prebuilt binary for $(uname -m); build from source: https://github.com/varogonz95/clawsh" ;;
	esac

	if [ -n "${CLAWSH_VERSION:-}" ]; then
		tag="v${CLAWSH_VERSION#v}"
	else
		tag=$(latest_tag)
	fi
	version=${tag#v}
	name="clawsh_${version}_${os}_${arch}"
	dir="${CLAWSH_INSTALL_DIR:-$HOME/.local/bin}"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "downloading clawsh $version for $os/$arch"
	fetch "$RELEASES/download/$tag/$name.tar.gz" "$tmp/$name.tar.gz" ||
		die "couldn't download $RELEASES/download/$tag/$name.tar.gz"
	fetch "$RELEASES/download/$tag/checksums.txt" "$tmp/checksums.txt" ||
		die "couldn't download $RELEASES/download/$tag/checksums.txt"

	want=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$name.tar.gz isn't listed in the release's checksums.txt"
	[ "$(sha256 "$tmp/$name.tar.gz")" = "$want" ] || die "$name.tar.gz failed its SHA-256 check; not installing"

	tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
	[ -f "$tmp/$name/clawsh" ] || die "the archive has no $name/clawsh"
	mkdir -p "$dir"
	# Copy next to the target, then rename: replaces a running clawsh safely.
	cp "$tmp/$name/clawsh" "$dir/.clawsh.new"
	chmod 755 "$dir/.clawsh.new"
	mv -f "$dir/.clawsh.new" "$dir/clawsh"
	say "installed $("$dir/clawsh" version 2>/dev/null || echo "clawsh $version") to $dir/clawsh"

	case ":$PATH:" in
		*":$dir:"*) ;;
		*) say "$dir isn't on your PATH; add it, e.g.:  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile" ;;
	esac
}

main "$@"
