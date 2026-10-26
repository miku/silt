#!/usr/bin/env bash
#
# Sends the same Docker Hub search several times and prints, per call, the
# hit count and hashes of the result order and the result set: identical
# requests, different answers. Then compares the hit counts of a shorter
# query.
#
# Usage: scripts/docker-hub.sh [QUERY], N=8 by default. Writes
# data/docker-hub.txt.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p data

query=${1:-container kubernetes logging}
n=${N:-8}
ua='silt-talk (https://github.com/miku/silt)'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# search QUERY prints the hit count, then the top 20 repository names, as
# silt asks for them (sources/dockerhub.go).
search() {
	curl -fsS -A "$ua" -G 'https://hub.docker.com/v2/search/repositories/' \
		--data-urlencode "query=$1" --data-urlencode page_size=20 |
		jq -r '.count, .results[].repo_name'
}

hash() { md5sum | cut -c1-8; }

{
	printf '# %s, %d identical searches for "%s"\n' "$(date -u +%FT%TZ)" "$n" "$query"
	for i in $(seq "$n"); do
		search "$query" >"$tmp/$i"
		printf '%2d  count=%s  order=%s  set=%s\n' "$i" "$(head -1 "$tmp/$i")" \
			"$(tail -n +2 "$tmp/$i" | hash)" "$(tail -n +2 "$tmp/$i" | sort | hash)"
		sleep 1
	done
	short=${query#* }
	printf '# hit counts: "%s" %s, "%s" %s\n' "$short" "$(search "$short" | head -1)" \
		"$query" "$(head -1 "$tmp/1")"
} | tee data/docker-hub.txt
