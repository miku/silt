// Command fetchmodel downloads the embedding model into silt's cache, where
// the model-dependent tests find it; they never download it themselves. It
// prints the directory, and does nothing more if the model is already there.
//
//	go run ./scripts/fetchmodel
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/miku/silt/internal/minilm"
	"github.com/miku/silt/rank"
	"github.com/miku/silt/sources"
)

func main() {
	log.SetFlags(0)
	dir, err := rank.ModelDir()
	if err != nil {
		log.Fatal(err)
	}
	if !minilm.IsComplete(dir) {
		lastPct := map[string]int{}
		err := minilm.Download(context.Background(), dir, sources.UserAgent(), func(file string, done, total int64) {
			if pct := int(done * 100 / total); pct/10 != lastPct[file]/10 {
				lastPct[file] = pct
				fmt.Fprintf(os.Stderr, "%s: %d%% of %d bytes\n", file, pct, total)
			}
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println(dir)
}
