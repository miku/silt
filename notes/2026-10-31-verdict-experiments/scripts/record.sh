#!/usr/bin/env bash
#
# Records silt's search results for every idea in ideas.tsv, once.
#
#   scripts/record.sh            # all ideas not yet in data/runs.jsonl
#   scripts/record.sh 001 033    # only these ids
#
# Run from the experiment directory. Per idea, silt runs twice:
#
#   1. with the embedding model, against the live registries;
#   2. with --keyword-only, answered from the result cache that run 1 just
#      filled, and without the sources that failed in run 1, so that both
#      runs rank the same candidates.
#
# --limit 10000 keeps every ranked candidate, not just the top 50, so that
# other scorers can rescore the whole set later. The runs use a cache of
# their own (work/cache), with the embedding model, the catalogs and the
# browser profile linked in from the usual one, so nothing is downloaded
# twice and no earlier search result leaks in.
#
# One JSON line per idea goes to data/runs.jsonl; the logs stay in work/.

set -euo pipefail

pause=${PAUSE:-3}
here=$(pwd)
root=$(git rev-parse --show-toplevel)
work=$here/work
out=$here/data/runs.jsonl
silt=$work/silt

mkdir -p "$work/logs" "$here/data"
touch "$out"

(cd "$root" && go build -o "$silt" ./cmd/silt)
version=$("$silt" --version)
commit=$(git -C "$root" rev-parse --short HEAD)

usual=${XDG_CACHE_HOME:-$HOME/.cache}/silt
mkdir -p "$work/cache/silt"
for name in all-MiniLM-L6-v2-5f1b8cd catalogs browser; do
	if [[ -e $usual/$name && ! -e $work/cache/silt/$name ]]; then
		ln -s "$usual/$name" "$work/cache/silt/$name"
	fi
done
export XDG_CACHE_HOME=$work/cache

first=1
tail -n +2 ideas.tsv | while IFS=$'\t' read -r id stratum target idea; do
	if [[ $# -gt 0 && ! " $* " =~ " $id " ]]; then
		continue
	fi
	if grep -q "^{\"id\":\"$id\"" "$out"; then
		continue
	fi
	[[ $first -eq 1 ]] || sleep "$pause"
	first=0

	recorded_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	start=$(date +%s.%N)
	"$silt" --json --fast --limit 10000 --cache-ttl 24h "$idea" </dev/null \
		>"$work/logs/$id.emb.json" 2>"$work/logs/$id.emb.log" || true
	seconds=$(echo "$(date +%s.%N) - $start" | bc)
	if ! jq -e .verdict "$work/logs/$id.emb.json" >/dev/null 2>&1; then
		# silt turns some ideas down before searching ("Too vague"). That
		# is a result too, so it is kept; any other failure is retried on
		# the next run.
		if grep -q '^Error: Too vague' "$work/logs/$id.emb.log"; then
			jq -c -n --arg id "$id" --arg stratum "$stratum" --arg target "$target" \
				--arg idea "$idea" --arg recorded_at "$recorded_at" --arg silt "$version" \
				--arg commit "$commit" --arg error "$(head -n 1 "$work/logs/$id.emb.log")" \
				'{id: $id, stratum: $stratum, target: $target, idea: $idea,
				  recorded_at: $recorded_at, silt: $silt, commit: $commit,
				  error: ($error | ltrimstr("Error: "))}' >>"$out"
			echo "$id  rejected: $idea"
		else
			echo "$id: no result, see $work/logs/$id.emb.log" >&2
		fi
		continue
	fi

	failed=$(jq -r '.verdict.sources_failed | join(",")' "$work/logs/$id.emb.json")
	"$silt" --json --fast --limit 10000 --cache-ttl 24h --keyword-only \
		${failed:+--exclude "$failed"} "$idea" </dev/null \
		>"$work/logs/$id.kw.json" 2>"$work/logs/$id.kw.log" || true

	jq -c -n \
		--arg id "$id" --arg stratum "$stratum" --arg target "$target" --arg idea "$idea" \
		--arg recorded_at "$recorded_at" --arg silt "$version" --arg commit "$commit" \
		--argjson seconds "$seconds" \
		--slurpfile emb "$work/logs/$id.emb.json" \
		--slurpfile kw "$work/logs/$id.kw.json" \
		'{id: $id, stratum: $stratum, target: $target, idea: $idea,
		  recorded_at: $recorded_at, silt: $silt, commit: $commit, seconds: $seconds,
		  embeddings: $emb[0], keyword_only: ($kw[0] // null)}' >>"$out"

	printf '%s  %-9s %-9s %4d matches  %5.1fs  %s\n' "$id" \
		"$(jq -r .verdict.level "$work/logs/$id.emb.json")" \
		"$(jq -r '.verdict.level // "-"' "$work/logs/$id.kw.json" 2>/dev/null || echo -)" \
		"$(jq '.matches | length' "$work/logs/$id.emb.json")" "$seconds" "$idea"
done
