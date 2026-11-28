#!/usr/bin/env bash
# pr-text.sh [base] [head]
#
# Writes the title (first line) and Markdown description of a pull request that
# merges head into base, from the commit messages. Prints nothing when head has
# no commits that base lacks. Preview what the "pull request" workflow will write:
#
#   scripts/pr-text.sh origin/main origin/develop
set -euo pipefail

base=${1:-origin/main}
head=${2:-origin/develop}
here=$(cd "$(dirname "$0")" && pwd)
limit=60000 # GitHub refuses descriptions over 65536 characters

commits=$(git rev-list --no-merges --reverse "$base..$head")
[ -n "$commits" ] || { echo "$head has nothing that $base lacks." >&2; exit 0; }
count=$(echo "$commits" | wc -l | tr -d ' ')

# type(scope)!: description
conventional='^([a-z]+)(\(([^)]*)\))?(!)?: (.+)$'

# kind <subject> sets type, scope, breaking and desc.
kind() {
  if [[ $1 =~ $conventional ]]; then
    type=${BASH_REMATCH[1]} scope=${BASH_REMATCH[3]} breaking=${BASH_REMATCH[4]} desc=${BASH_REMATCH[5]}
  else
    type=other scope="" breaking="" desc=$1
  fi
  case $type in
    feat | fix | perf | refactor | docs | test | ci | build | chore) ;;
    *) type=other ;;
  esac
}

# An @name from a commit message must not notify anybody.
quiet() { sed -E 's/(^|[^A-Za-z0-9_`.])@([A-Za-z0-9][A-Za-z0-9_\/-]*)/\1`@\2`/g'; }

# --- title: the first feature, else the first fix, else the first commit -------
lead=""
for want in feat fix ""; do
  for c in $commits; do
    kind "$(git log -1 --format=%s "$c")"
    if [ -z "$want" ] || [ "$type" = "$want" ]; then
      lead=$desc
      break
    fi
  done
  [ -n "$lead" ] && break
done
more=""
[ "$count" -gt 1 ] && more=" (+$((count - 1)) more)"
room=$((72 - ${#more}))
[ ${#lead} -gt $room ] && lead="${lead:0:$((room - 3))}..."
first=$(printf '%s' "${lead:0:1}" | tr '[:lower:]' '[:upper:]')
printf '%s%s%s\n' "$first" "${lead:1}" "$more" | quiet

# --- description ----------------------------------------------------------------

# A commit body without trailers, with hard-wrapped lines joined again.
body_of() {
  git log -1 --format=%b "$1" |
    grep -viE '^(co-authored-by|signed-off-by|claude-session):' |
    awk '
      function flush() { if (text != "") { if (blank) print ""; print "  " text; text = ""; blank = 0 } }
      /^[[:space:]]*$/      { flush(); blank = 1; next }
      /^[[:space:]]*([-*]|[0-9]+\.) / || /^[[:space:]]/ { flush(); text = $0; next }
      { text = (text == "" ? $0 : text " " $0) }
      END { flush() }' || true
}

# section <heading> <type|breaking> <with bodies: yes|no>
section() {
  local c found="" label body
  for c in $commits; do
    kind "$(git log -1 --format=%s "$c")"
    if [ -n "$breaking" ]; then
      [ "$2" = breaking ] || continue
    else
      [ "$2" = "$type" ] || continue
    fi
    [ -n "$found" ] || printf '\n## %s\n\n' "$1"
    found=yes
    label=""
    [ -n "$scope" ] && label="**$scope:** "
    printf -- '- %s%s (%s)\n' "$label" "$desc" "$(git rev-parse --short "$c")"
    if [ "$3" = yes ]; then
      body=$(body_of "$c")
      [ -n "$body" ] && printf '\n%s\n\n' "$body"
    fi
  done
}

# What merging will publish: judged on the merged files, by the release rule.
release_line() {
  local tree why version
  if ! tree=$(git merge-tree --write-tree "$base" "$head" 2>/dev/null); then
    echo "**Release:** unknown until the merge conflicts are resolved."
    return
  fi
  why=$(mktemp)
  version=$("$here/release-version.sh" "$tree" patch 2>"$why")
  if [ -n "$version" ]; then
    echo "**Release:** merging publishes **$version**. $(cat "$why")"
    echo "Add the label \`release:minor\` before merging to raise the minor number instead."
  else
    echo "**Release:** merging publishes nothing. $(cat "$why")"
  fi
  rm -f "$why"
}

describe() {
  local noun=commits
  [ "$count" -eq 1 ] && noun=commit
  echo "Merges $count $noun from \`${head#origin/}\` into \`${base#origin/}\`."
  echo
  release_line
  section "Breaking changes" breaking "$1"
  section "Features" feat "$1"
  section "Fixes" fix "$1"
  section "Performance" perf "$1"
  section "Refactoring" refactor "$1"
  section "Tests" test "$1"
  section "CI" ci "$1"
  section "Build" build "$1"
  section "Documentation" docs "$1"
  section "Chores" chore "$1"
  section "Other changes" other "$1"
  printf '\n## Files changed\n\n```text\n'
  git diff --stat=90 "$base...$head" | tail -n 60
  printf '```\n'
}

text=$(describe yes | cat -s | quiet)
[ ${#text} -le $limit ] || text=$(describe no | cat -s | quiet)
echo
printf '%s\n' "${text:0:$limit}"
