package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	applicationembedding "github.com/shiyudesu/frux/internal/application/embedding"
)

func main() {
	input := flag.String("input", "testdata/multimodal/golden-v1.json", "versioned multimodal golden-set path")
	flag.Parse()
	content, err := os.ReadFile(*input)
	if err != nil {
		fail(err)
	}
	var golden applicationembedding.MultimodalGoldenSet
	if err := json.Unmarshal(content, &golden); err != nil {
		fail(err)
	}
	report, err := applicationembedding.EvaluateMultimodalGoldenSet(golden)
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(encoded))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
