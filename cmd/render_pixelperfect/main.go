package main

import (
	"flag"
	"fmt"
	"os"

	"cooked/pixelrender"
)

func fail(stage string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", stage, err)
	os.Exit(1)
}

func main() {
	jsonPath := flag.String("json", "", "path to json file")
	tmplPath := flag.String("tmpl", "", "path to html template (.tmpl)")
	outPath := flag.String("out", "", "output html file")
	flag.Parse()

	if *jsonPath == "" || *tmplPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: go run render_pixelperfect.go -json json.json -tmpl template.html.tmpl -out out.html")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*jsonPath)
	if err != nil {
		fail("read json", err)
	}

	recipe, err := pixelrender.ParseRecipeJSON(raw)
	if err != nil {
		fail("parse json", err)
	}

	tplBytes, err := os.ReadFile(*tmplPath)
	if err != nil {
		fail("read template", err)
	}

	htmlBytes, err := pixelrender.Render(recipe, tplBytes)
	if err != nil {
		fail("render", err)
	}

	if err := os.WriteFile(*outPath, htmlBytes, 0644); err != nil {
		fail("write output", err)
	}
}
