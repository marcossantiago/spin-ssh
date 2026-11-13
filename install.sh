#!/bin/sh
# Installs spin-ssh from GitHub Releases:
#
#   curl -fsSL https://raw.githubusercontent.com/marcossantiago/spin-ssh/main/install.sh | sh
#
# SPIN_SSH_VERSION=v0.2.0 picks a release (default: the latest).
# SPIN_SSH_INSTALL_DIR picks the folder (default: ~/.local/bin, no sudo needed).
set -eu

repo="marcossantiago/spin-ssh"
install_dir="${SPIN_SSH_INSTALL_DIR:-$HOME/.local/bin}"
version="${SPIN_SSH_VERSION:-}"

say() { printf '\033[1;35m›\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

for cmd in curl tar uname mktemp; do
	command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is required"
done

case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "spin-ssh is built for macOS and Linux, not $(uname -s)" ;;
esac
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "spin-ssh is built for amd64 and arm64, not $(uname -m)" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	fail "sha256sum or shasum is required to verify the download"
fi

if [ -z "$version" ]; then
	# The latest release's page redirects to .../releases/tag/<version>. This avoids
	# the GitHub API and its rate limit.
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "couldn't reach GitHub"
	version="${url##*/}"
	case "$version" in
		v*) ;;
		*) fail "no spin-ssh release found at https://github.com/$repo/releases" ;;
	esac
fi

archive="spin-ssh_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading spin-ssh $version for $os/$arch"
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "couldn't download $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "couldn't download checksums.txt"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "$archive isn't in checksums.txt"
[ "$(sha256 "$tmp/$archive")" = "$expected" ] || fail "$archive doesn't match its checksum; not installing it"

tar -xzf "$tmp/$archive" -C "$tmp" spin-ssh
mkdir -p "$install_dir"
# Move into place in one step, so a running spin-ssh is never half-written.
cp "$tmp/spin-ssh" "$install_dir/.spin-ssh.new"
chmod 0755 "$install_dir/.spin-ssh.new"
mv -f "$install_dir/.spin-ssh.new" "$install_dir/spin-ssh"

printf '\033[1;32m✓\033[0m Installed %s to %s\n' "$("$install_dir/spin-ssh" --version)" "$install_dir/spin-ssh"

case ":$PATH:" in
	*":$install_dir:"*) ;;
	*)
		case "${SHELL:-}" in
			*/zsh) rc="$HOME/.zshrc" ;;
			*/bash) rc="$HOME/.bashrc" ;;
			*) rc="your shell's startup file" ;;
		esac
		say "$install_dir isn't on your PATH. Add this line to $rc, then open a new terminal:"
		printf '\n  export PATH="%s:$PATH"\n\n' "$install_dir"
		;;
esac

say "Run spin-ssh to get started. Update later with: spin-ssh update"
