#!/bin/sh
# Records patent's `--fast --json` output for each idea in
# testdata/embeddings/differential-ideas.txt into
# testdata/embeddings/differential.jsonl, one run per line, for the
# differential test in rank. It hits the live registries; the pause keeps
# unauthenticated GitHub search under its 10 requests a minute.
#
#   sh scripts/patent-harness/record-differential.sh [path/to/patent]
set -eu
patent=${1:-patent}
cd "$(dirname "$0")/../.."
out=testdata/embeddings/differential.jsonl
: > "$out"
while IFS= read -r idea; do
	# The exit code is the verdict level, so it is not an error.
	"$patent" --fast --json "$idea" 2>/dev/null | python3 -I -c 'import json, sys; print(json.dumps(json.load(sys.stdin), ensure_ascii=False))' >> "$out" || true
	sleep 7
done < testdata/embeddings/differential-ideas.txt
wc -l "$out"
