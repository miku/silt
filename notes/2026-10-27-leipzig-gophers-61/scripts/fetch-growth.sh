#!/usr/bin/env bash
#
# Package counts over time. No registry publishes its history, so read old
# snapshots from the Wayback Machine, plus today's live values:
#
#   crates.io  num_crates in https://crates.io/api/v1/summary
#   PyPI       "N projects" on https://pypi.org/
#   Packagist  totals.packages in https://packagist.org/statistics.json
#   npm        doc_count in https://replicate.npmjs.com/ (not plotted: spam
#              waves and a backend change in 2026 make it incomparable)
#
# The snapshots are pinned, so a rerun reads the same pages. They were picked
# from the CDX index, e.g. one per month:
#
#   curl 'https://web.archive.org/cdx/search/cdx?url=crates.io/api/v1/summary&fl=timestamp&filter=statuscode:200&collapse=timestamp:6'
#
# Writes data/growth.tsv: registry, date, count.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p data

ua='silt-talk (https://github.com/miku/silt)'

crates=(20181222030143 20191210124424 20201210161600 20211214204127 20221220210617
	20231208071127 20241205223026 20250701141150 20251201160934 20260101052015
	20260202071512 20260303101141 20260401152637 20260501182723 20260603170545
	20260701140047 20260801002942 20260901104826 20261005143716)
pypi=(20190103232223 20200101100443 20210101135103 20220103195923 20230101003220
	20240101002053 20250101053616 20260101103427)
packagist=(20190818084015 20200807115455 20210120125404 20220124191803 20230104123905
	20240303045202 20250119090541 20260116022038)
npm=(20181226124009 20190717002147 20200807203007 20210124143223 20220117092750
	20230105125426 20240303082246 20250116073902 20260106071640 20260304043920
	20260515191549)

# wayback TIMESTAMP URL prints the archived body. Snapshots are often stored
# gzipped, hence --compressed. The Wayback Machine refuses connections after
# a burst of requests, so fetches are spaced out, retried, and cached.
cache=${XDG_CACHE_HOME:-$HOME/.cache}/silt-talk/wayback
mkdir -p "$cache"
wayback() {
	local file
	file=$cache/$1-$(printf '%s' "$2" | md5sum | cut -c1-8)
	if [[ ! -s $file ]]; then
		sleep 2
		curl -fsSL --compressed --retry 5 --retry-connrefused --retry-delay 10 \
			-A "$ua" "https://web.archive.org/web/${1}id_/${2}" >"$file.tmp" &&
			mv "$file.tmp" "$file"
	fi
	cat "$file"
}

# The extractors read a page on stdin and print the count.
crates_count() { jq -r '.num_crates'; }
pypi_count() { grep -oE '[0-9][0-9,]* projects' | head -1 | tr -dc '0-9'; }
packagist_count() { jq -r '.totals.packages'; }
npm_count() { jq -r '.doc_count'; }

# row REGISTRY DATE COUNT prints a row, or warns when there is no count.
row() {
	if [[ $3 =~ ^[0-9]+$ ]]; then
		printf '%s\t%s\t%s\n' "$1" "$2" "$3"
	else
		echo "fetch-growth: no count for $1 at $2" >&2
	fi
}

# history REGISTRY URL EXTRACTOR TIMESTAMP... prints one row per snapshot.
history() {
	local registry=$1 url=$2 extract=$3 ts
	shift 3
	for ts in "$@"; do
		row "$registry" "${ts:0:4}-${ts:4:2}-${ts:6:2}" "$(wayback "$ts" "$url" | $extract || true)"
	done
}

# live REGISTRY URL EXTRACTOR prints today's row.
live() {
	row "$1" "$(date -u +%F)" "$(curl -fsSL --compressed -A "$ua" "$2" | $3 || true)"
}

{
	printf '# registry\tdate\tcount\n'
	history crates.io https://crates.io/api/v1/summary crates_count "${crates[@]}"
	live crates.io https://crates.io/api/v1/summary crates_count
	history pypi https://pypi.org/ pypi_count "${pypi[@]}"
	live pypi https://pypi.org/ pypi_count
	history packagist https://packagist.org/statistics.json packagist_count "${packagist[@]}"
	live packagist https://packagist.org/statistics.json packagist_count
	history npm https://replicate.npmjs.com/ npm_count "${npm[@]}"
	live npm https://replicate.npmjs.com/ npm_count
} >data/growth.tsv

wc -l <data/growth.tsv | xargs printf '%s lines in data/growth.tsv\n'
