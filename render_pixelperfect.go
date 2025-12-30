package main

import (
	"html/template"
	"regexp"
	"strings"
)

type ViewModel struct {
	Recipe RecipeVM
}

type NutritionVM struct {
	Label string
	Value string
	Unit  string
}

type RecipeVM struct {
	Title         string
	RecipeName    string
	RecipeNameMin string
	Description   string
	URL           string
	Image         string

	PrepTime   string
	Difficulty string
	Origin     string

	Tags      []string
	Utensils  []string
	Allergens []string
	Nutrition []NutritionVM

	Ingredients1P []ScrapedIngredient
	Steps         []ScrapedStep
}

var unitFromParens = regexp.MustCompile(`\(([^)]+)\)\s*$`)

func inferUnit(label string) string {
	if m := unitFromParens.FindStringSubmatch(label); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "sel":
		return "g"
	default:
		return "g"
	}
}

func toNutritionVM(items []NutritionItem) []NutritionVM {
	out := make([]NutritionVM, 0, len(items))
	for _, it := range items {
		out = append(out, NutritionVM{
			Label: it.Label,
			Value: it.Value,
			Unit:  inferUnit(it.Label),
		})
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func BuildDetailViewModel(doc RecipeDocument) ViewModel {
	return ViewModel{
		Recipe: RecipeVM{
			Title:         firstNonEmpty(doc.Title, doc.RecipeName),
			RecipeName:    firstNonEmpty(doc.RecipeName, doc.Title),
			RecipeNameMin: doc.RecipeNameMin,
			Description:   doc.Description,
			URL:           doc.URL,
			Image:         doc.Image,
			PrepTime:      doc.PrepTime,
			Difficulty:    doc.Difficulty,
			Origin:        doc.Origin,
			Tags:          doc.Tags,
			Utensils:      doc.Utensils,
			Allergens:     doc.Allergens,
			Nutrition:     toNutritionVM(doc.Nutrition),
			Ingredients1P: doc.Ingredients1P,
			Steps:         doc.Steps,
		},
	}
}

func parseTemplateFromFile(path string) (*template.Template, error) {
	return template.New("page").Option("missingkey=error").ParseFiles(path)
}
