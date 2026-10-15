# Helpers shared by release-prepare.yml and release-hotfix.yml (bash, sourced).
# The workflows copy this file to $RUNNER_TEMP right after checking out main,
# because the commits they later check out may predate it.

SEMVER_RE='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'

# core 1.2.3-rc.1 -> 1.2.3
core() { echo "${1%%-*}"; }

# version_gt A B: A's major.minor.patch is greater than B's. Pre-release
# suffixes are ignored: callers compare against stable tags only, and sort -V
# alone doesn't order suffixes the semver way.
version_gt() {
  local a b
  a=$(core "$1") b=$(core "$2")
  [[ $a != "$b" && $(printf '%s\n%s\n' "$a" "$b" | sort -V | tail -1) == "$a" ]]
}

# highest_stable [glob]: the highest stable version among local tags matching
# glob (default v*), without the v. Empty when there is none.
highest_stable() {
  git tag -l "${1:-v*}" | sed 's/^v//' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true
}

# bump_plugin VERSION: set the top-level "version" of plugin.json, keeping the
# file's hand-formatted layout. Only the line indented as a top-level key is
# touched, and anything but that one line changing is an error.
bump_plugin() {
  local f=plugin/.claude-plugin/plugin.json
  sed -i -E "s/^(  \"version\": \")[^\"]*(\")/\1$1\2/" "$f"
  if ! jq -e --arg v "$1" '.version == $v' "$f" >/dev/null; then
    echo "::error file=$f::could not set the top-level version to $1"
    return 1
  fi
  local n
  n=$(git diff --numstat -- "$f" | cut -f1,2)
  if [[ -n $n && $n != $'1\t1' ]]; then
    echo "::error file=$f::setting the version changed more than one line"
    return 1
  fi
}
