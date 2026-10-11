set -euo pipefail
umask 077
version="$1"
health_url="$2"
fail() { printf 'orbit: %s\n' "$*" >&2; exit 1; }
[[ "$(id -u)" = 0 ]] || fail 'update requires root or sudo'
case "$(uname -s)" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *) fail 'unsupported remote operating system' ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail 'unsupported remote architecture' ;;
esac
restart() {
  if [[ "$platform" = linux ]]; then
    systemctl restart stardust.service
  else
    launchctl kickstart -k system/com.ch55secake.stardust
  fi
}
if [[ "$platform" = linux ]]; then
  [[ "$(systemctl show stardust.service -p LoadState --value)" = loaded ]] || fail 'existing stardust.service required'
  binary="$(systemctl show stardust.service -p ExecStart --value | sed -n 's/.*path=\(.*\) ; argv\[\]=.*/\1/p')"
else
  launchctl print system/com.ch55secake.stardust >/dev/null || fail 'existing Stardust LaunchDaemon required'
  binary="$(/usr/libexec/PlistBuddy -c 'Print :ProgramArguments:0' /Library/LaunchDaemons/com.ch55secake.stardust.plist)"
fi
[[ "$binary" = /*/stardust && "$binary" != *$'\n'* ]] || fail 'service must directly execute an absolute stardust binary path'
[[ -f "$binary" && ! -L "$binary" ]] || fail 'existing regular binary required; package-managed symlinks are not updated'
command -v curl >/dev/null || fail 'curl is required on the remote host'
command -v tar >/dev/null || fail 'tar is required on the remote host'
tmp="$(mktemp -d "${TMPDIR:-/tmp}/orbit-stardust.XXXXXX")"
staged=''
changed=false
cleanup() {
  result=$?
  trap - EXIT
  if [[ "$result" != 0 && "$changed" = true ]]; then
    printf 'orbit: update failed; restoring previous binary\n' >&2
    if cp -p "$tmp/previous" "$staged" && mv -f "$staged" "$binary"; then
      restart || printf 'orbit: rollback restart failed; inspect the service logs\n' >&2
    else
      printf 'orbit: rollback failed; backup retained at %s/previous\n' "$tmp" >&2
      exit "$result"
    fi
  fi
  [[ -z "$staged" ]] || rm -f "$staged"
  rm -rf "$tmp"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
download() {
  curl --fail --location --silent --show-error --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 90 --retry 2 --output "$1" "$2"
}
repo='https://github.com/ch55secake/stardust/releases'
if [[ "$version" = latest ]]; then
  release="$repo/latest/download"
else
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'invalid release version'
  release="$repo/download/$version"
fi
download "$tmp/checksums" "$release/checksums.txt"
archive="$(awk -v os="$platform" -v arch="$arch" '$2 ~ ("^stardust_[0-9]+\\.[0-9]+\\.[0-9]+_" os "_" arch "\\.tar\\.gz$") {print $2; exit}' "$tmp/checksums")"
pattern="^stardust_([0-9]+\.[0-9]+\.[0-9]+)_${platform}_${arch}\.tar\.gz$"
[[ "$archive" =~ $pattern ]] || fail 'release has no compatible archive'
tag="v${BASH_REMATCH[1]}"
[[ "$version" = latest || "$version" = "$tag" ]] || fail 'archive does not match requested release'
checksum="$(awk -v asset="$archive" '$2 == asset {print $1; exit}' "$tmp/checksums")"
[[ "$checksum" =~ ^[[:xdigit:]]{64}$ ]] || fail 'invalid release checksum'
# Pin the archive URL after reading latest checksums, avoiding a latest-release race.
download "$tmp/archive" "$repo/download/$tag/$archive"
if command -v sha256sum >/dev/null; then
  actual="$(sha256sum "$tmp/archive" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/archive" | awk '{print $1}')"
fi
actual="$(printf '%s' "$actual" | tr 'A-F' 'a-f')"
checksum="$(printf '%s' "$checksum" | tr 'A-F' 'a-f')"
[[ "$actual" = "$checksum" ]] || fail 'checksum verification failed'
tar -xzf "$tmp/archive" -C "$tmp" stardust
[[ -f "$tmp/stardust" && ! -L "$tmp/stardust" ]] || fail 'archive must contain a regular stardust binary'
cp -p "$binary" "$tmp/previous"
staged="$(mktemp "$(dirname "$binary")/.stardust-update.XXXXXX")"
install -m 0755 "$tmp/stardust" "$staged"
changed=true
mv -f "$staged" "$binary"
restart
healthy=false
for attempt in {1..10}; do
  if response="$(curl --fail --silent --show-error --noproxy '*' --max-time 3 "$health_url" 2>/dev/null)" &&
    printf '%s' "$response" | grep -Eq '"api_version"[[:space:]]*:[[:space:]]*"v1"' &&
    printf '%s' "$response" | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'; then
    healthy=true
    break
  fi
  sleep 1
done
[[ "$healthy" = true ]] || fail 'health check failed after restart'
if [[ "$platform" = linux ]]; then
  systemctl is-active --quiet stardust.service || fail 'service is not active after update'
else
  launchctl print system/com.ch55secake.stardust | grep -Eq 'pid = [0-9]+' || fail 'LaunchDaemon is not running after update'
fi
changed=false
printf 'Updated Stardust to %s; service configuration preserved and health verified.\n' "$tag"
