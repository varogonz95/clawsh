# Helpers shared by release-cut.yml and release-hotfix.yml (bash, sourced).
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

# next_version LAST BUMP: LAST (x.y.z) bumped by patch, minor or major.
next_version() {
  local maj min pat
  IFS=. read -r maj min pat <<< "$(core "$1")"
  case $2 in
    major) echo "$((maj + 1)).0.0" ;;
    minor) echo "$maj.$((min + 1)).0" ;;
    patch) echo "$maj.$min.$((pat + 1))" ;;
    *) echo "::error::unknown bump '$2'" >&2; return 1 ;;
  esac
}
