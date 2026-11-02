package sources

import "github.com/miku/silt/internal/model"

// SizesAsOf is the day the sizes in [Size] were counted.
const SizesAsOf = "2026-11-03"

// sizes are how many packages each package source held on SizesAsOf, as
// scripts/sizes.sh counts them: from the registry itself where it publishes a
// count, and from packages.ecosyste.ms where it doesn't (Go and Hex). They
// are what a source's search covers, not what silt fetches from it, and they
// grow by the day, so they are only ever shown rounded.
var sizes = map[model.Source]int{
	model.CratesIO:          368_156,    // /api/v1/summary num_crates
	model.Npm:               4_561_160,  // replicate.npmjs.com doc_count
	model.PyPI:              930_149,    // projects in the simple index
	model.Go:                2_541_189,  // modules on proxy.golang.org, per ecosyste.ms
	model.Maven:             661_801,    // groupId:artifactId pairs on Maven Central
	model.RubyGems:          198_247,    // /stats, total gems
	model.DockerHub:         13_615_962, // repositories, per the v3 catalog search
	model.VSCodeMarketplace: 123_917,    // published VS Code extensions
	model.JetBrains:         10_000,     // a lower bound: the search caps its total there
	model.NuGet:             504_837,    // listed packages, per the search API
	model.Homebrew:          16_502,     // formulae and casks
	model.Packagist:         478_349,    // /statistics.json
	model.Hex:               24_119,     // per ecosyste.ms
	model.ArtifactHub:       22_086,     // /api/v1/stats, every kind
	model.AUR:               123_742,    // package names, packages.gz
	model.Hackage:           19_620,     // /packages/
	model.Nixpkgs:           148_690,    // packages in the unstable channel's index
	model.ArchLinux:         15_661,     // core, extra and multilib
}

// Size is how many packages s held on [SizesAsOf], roughly. It is false for
// the sources that are not package registries: GitHub, Hacker News and
// Codeberg.
func Size(s model.Source) (int, bool) {
	n, ok := sizes[s]
	return n, ok
}
