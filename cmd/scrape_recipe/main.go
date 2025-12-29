package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"cooked/scraper"
)

func main() {
	urlFlag := flag.String("url", "", "hfresh recipe URL")
	outFlag := flag.String("out", "recipe.json", "output JSON file")
	flag.Parse()

	if *urlFlag == "" {
		fmt.Fprintln(os.Stderr, "Error: -url is required")
		os.Exit(2)
	}

	payload, err := scraper.ScrapeRecipe(context.Background(), *urlFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fetch error:", err)
		os.Exit(1)
	}

	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "JSON marshal error:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outFlag, out, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Write error:", err)
		os.Exit(1)
	}

	fmt.Println("OK ->", *outFlag)
	fmt.Printf("facts: prep_time=%q difficulty=%q origin=%q recipe_name_min=%q nutrition_items=%d\n",
		payload.PrepTime, payload.Difficulty, payload.Origin, payload.RecipeNameMin, len(payload.Nutrition))
}
