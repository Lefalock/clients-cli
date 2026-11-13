#!/bin/sh
# SPDX-License-Identifier: MPL-2.0
# Install the public Lefalock release for the current user.
set -eu
umask 077

fail() { printf 'Lefalock: %s\n' "$*" >&2; exit 1; }
version='v0.0.15'
if [ "$#" -eq 2 ] && [ "$1" = '--version' ]; then
  version=$2
elif [ "$#" -ne 0 ]; then
  fail 'Usage: sh install.sh [--version vX.Y.Z]'
fi
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' ||
  fail 'Download install.sh from a release, or specify --version vX.Y.Z.'
: "${HOME:?HOME must point to your user directory}"
for tool in curl tar awk grep mktemp uname; do
  command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
done
case "$(uname -s)" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *) fail 'This installer supports Linux and macOS.' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) architecture=amd64 ;;
  aarch64|arm64) architecture=arm64 ;;
  *) fail 'Supported architectures: amd64 and arm64.' ;;
esac
case "${SHELL:-/bin/sh}" in
  */bash|*/zsh|*/sh|*/ksh|*/fish) ;;
  *) fail 'Supported shells: bash, zsh, sh, ksh, fish. Select one in SHELL.' ;;
esac
if command -v sha256sum >/dev/null 2>&1; then
  hash_file() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  hash_file() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  fail 'Install sha256sum or shasum before continuing.'
fi

temporary=$(mktemp -d)
staged=''
trap 'rm -rf "$temporary"; if [ -n "$staged" ]; then rm -f "$staged"; fi' EXIT
trap 'exit 1' HUP INT TERM
archive="lefalock_${version}_${platform}_${architecture}.tar.gz"
base="https://github.com/Lefalock/clients-cli/releases/download/$version"
download() {
  curl --fail --silent --show-error --location \
    --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --connect-timeout 15 --max-time 600 --max-filesize 134217728 \
    --output "$2" "$1"
}
printf 'Downloading Lefalock %s (%s/%s)...\n' "$version" "$platform" "$architecture"
download "$base/SHA256SUMS" "$temporary/SHA256SUMS"
download "$base/$archive" "$temporary/$archive"
expected=$(awk -v name="$archive" '
  $2 == name { count++; hash=$1 }
  END { if (count != 1 || length(hash) != 64 || hash ~ /[^0-9a-fA-F]/) exit 1; print tolower(hash) }
' "$temporary/SHA256SUMS") || fail 'Missing, duplicate or invalid archive checksum.'
actual=$(hash_file "$temporary/$archive")
[ "$actual" = "$expected" ] || fail 'SHA256 mismatch. Nothing was installed.'

# Accept only the two regular files produced by the release builder.
tar -tzf "$temporary/$archive" > "$temporary/names"
awk '
  $0 == "lefalock" { binary++; next }
  $0 == "README.md" { readme++; next }
  { invalid=1 }
  END { exit (invalid || binary != 1 || readme != 1) }
' "$temporary/names" || fail 'Unexpected release archive contents.'
tar -tvzf "$temporary/$archive" > "$temporary/types"
awk 'substr($0,1,1) != "-" { exit 1 }' "$temporary/types" ||
  fail 'The release archive must contain only regular files.'
mkdir "$temporary/extracted"
tar -xzf "$temporary/$archive" -C "$temporary/extracted" lefalock README.md

install_directory="$HOME/.local/opt/lefalock"
[ ! -L "$install_directory" ] || fail 'Installation directory must not be a symlink.'
mkdir -p "$install_directory"
for name in lefalock README.md; do
  target="$install_directory/$name"
  [ ! -L "$target" ] || fail "Installation target is a symlink: $name"
  if [ -e "$target" ] && [ ! -f "$target" ]; then
    fail "Installation target is not a regular file: $name"
  fi
done
staged=$(mktemp "$install_directory/.lefalock-install.XXXXXX")
cp "$temporary/extracted/lefalock" "$staged"
chmod 0755 "$staged"
mv -f "$staged" "$install_directory/lefalock"
staged=''
cp "$temporary/extracted/README.md" "$install_directory/README.md"
chmod 0644 "$install_directory/README.md"

path_line='export PATH="$HOME/.local/opt/lefalock:$PATH"'
append_path() {
  config_file=$1
  mkdir -p "$(dirname "$config_file")"
  if [ ! -f "$config_file" ] || ! grep -Fqx "$path_line" "$config_file"; then
    printf '\n# Lefalock\n%s\n' "$path_line" >> "$config_file"
  fi
}
case "${SHELL:-/bin/sh}" in
  */zsh) append_path "${ZDOTDIR:-$HOME}/.zshrc" ;;
  */bash)
    append_path "$HOME/.bashrc"
    if [ -f "$HOME/.bash_profile" ]; then
      append_path "$HOME/.bash_profile"
    elif [ -f "$HOME/.bash_login" ]; then
      append_path "$HOME/.bash_login"
    else
      append_path "$HOME/.profile"
    fi
    ;;
  */fish)
    path_line='set -gx PATH "$HOME/.local/opt/lefalock" $PATH'
    append_path "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
    ;;
  *) append_path "$HOME/.profile" ;;
esac
printf '\nInstalled Lefalock %s in %s\n' "$version" "$install_directory"
case "${SHELL:-/bin/sh}" in
  */sh|*/ksh)
    printf '.profile is read by login shells; a new terminal tab may not load it.\n'
    printf 'Apply PATH in your current shell:\n%s\n' "$path_line"
    printf 'Then run: lefalock\n'
    ;;
  *) printf 'Open a new terminal, then run: lefalock\n' ;;
esac
