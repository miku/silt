#!/usr/bin/env bash
#
# Package counts per registry from the ecosyste.ms packages API. The API
# answers browser-like requests with HTTP 402, so send a JSON Accept header.
#
# Writes data/ecosystems.json (all registries) and data/ecosystems-top10.tsv
# (the ten largest default registries, for plots/ecosystems.gp).
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p data

curl -fsS -A 'silt-talk (https://github.com/miku/silt)' -H 'Accept: application/json' \
	'https://packages.ecosyste.ms/api/v1/registries?per_page=200' >data/ecosystems.json

{
	printf '# fetched %s from packages.ecosyste.ms\n' "$(date -u +%F)"
	printf '# registry\tpackages\n'
	jq -r '
		{"npmjs.org": "npm", "proxy.golang.org": "Go modules", "hub.docker.com": "Docker Hub",
		 "pypi.org": "PyPI", "nuget.org": "NuGet", "huggingface.co": "Hugging Face",
		 "repo1.maven.org": "Maven Central", "packagist.org": "Packagist",
		 "crates.io": "crates.io", "rubygems.org": "RubyGems"} as $names
		| [.[] | select(.default)] | sort_by(-.packages_count) | .[:10][]
		| [($names[.name] // .name), .packages_count] | @tsv' data/ecosystems.json
} >data/ecosystems-top10.tsv

jq -r '"\(length) registries, \([.[].packages_count] | add) packages"' data/ecosystems.json
