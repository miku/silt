#!/usr/bin/env bash
#
# Prints how many packages each package source holds, for the table in
# internal/sources/sizes.go: from the registry itself where it publishes a
# count, from packages.ecosyste.ms where it doesn't (Go, Hex). GitHub, Hacker
# News and Codeberg are not package registries and have no size.
#
# Downloads Homebrew's catalogs, about 51 MB. Prints "source<TAB>count".
set -euo pipefail

ua='silt-sizes (https://github.com/miku/silt)'
get() { curl -fsS --max-time 120 -A "$ua" "$@"; }
row() { printf '%s\t%s\n' "$1" "$2"; }
ecosystems() { get -H 'Accept: application/json' "https://packages.ecosyste.ms/api/v1/registries/$1" | jq .packages_count; }

row crates-io "$(get https://crates.io/api/v1/summary | jq .num_crates)"
row npm "$(get https://replicate.npmjs.com/ | jq .doc_count)"
row pypi "$(get -H 'Accept: application/vnd.pypi.simple.v1+json' https://pypi.org/simple/ | jq '.projects | length')"
row go "$(ecosystems proxy.golang.org)"
row maven "$(get 'https://search.maven.org/solrsearch/select?q=*:*&rows=0&wt=json' | jq .response.numFound)"
row rubygems "$(get https://rubygems.org/stats | grep -A1 'Total gems</span>' | tail -1 | grep -o '>[0-9][0-9,]*<' | tr -d '<>,')"
row docker-hub "$(get 'https://hub.docker.com/api/search/v3/catalog/search?query=&from=0&size=1' | jq .total)"
# Packages for VS Code, not for Visual Studio, less the unpublished ones.
row vscode-marketplace "$(get -X POST https://marketplace.visualstudio.com/_apis/public/gallery/extensionquery \
	-H 'Content-Type: application/json' -H 'Accept: application/json;api-version=3.0-preview.1' \
	-d '{"filters":[{"criteria":[{"filterType":8,"value":"Microsoft.VisualStudio.Code"},{"filterType":12,"value":"4096"}],"pageSize":1}],"flags":0}' |
	jq '.results[0].resultMetadata[] | select(.metadataType == "ResultCount") | .metadataItems[0].count')"
# The search caps its total at 10000, so this is a lower bound.
row jetbrains "$(get 'https://plugins.jetbrains.com/api/searchPlugins?max=1' | jq .total)"
row nuget "$(get 'https://azuresearch-usnc.nuget.org/query?q=&take=1' | jq .totalHits)"
row homebrew "$(($(get https://formulae.brew.sh/api/formula.json | jq length) + $(get https://formulae.brew.sh/api/cask.json | jq length)))"
row packagist "$(get https://packagist.org/statistics.json | jq .totals.packages)"
row hex "$(ecosystems hex.pm)"
row artifact-hub "$(get -H 'Accept: application/json' https://artifacthub.io/api/v1/stats | jq .packages.total)"
row aur "$(get https://aur.archlinux.org/packages.gz | gunzip | grep -vc '^#')"
row hackage "$(get -H 'Accept: application/json' https://hackage.haskell.org/packages/ | jq length)"
# The index and credentials of internal/sources/nixpkgs.go.
row nixpkgs "$(get -u aWVSALXpZv:X8gPHnzL52wFEekuxsfQ9cSh -H 'Content-Type: application/json' \
	-d '{"query":{"term":{"type":"package"}}}' https://search.nixos.org/backend/latest-50-nixos-unstable/_count | jq .count)"
row arch-linux "$(get 'https://archlinux.org/packages/search/json/?repo=Core&repo=Extra&repo=Multilib&limit=1' | jq .count)"
