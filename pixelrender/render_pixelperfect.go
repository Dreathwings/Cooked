package pixelrender

import (
	"bytes"
	"encoding/json"
	"html/template"
	"regexp"
	"strings"
)

// RecipeJSON mirrors the scraper payload.
type RecipeJSON struct {
	Title         string `json:"title"`
	RecipeName    string `json:"recipe_name"`
	RecipeNameMin string `json:"recipe_name_min"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Image         string `json:"image"`

	PrepTime   string `json:"prep_time"`
	Difficulty string `json:"difficulty"`
	Origin     string `json:"origin"`

	Tags      []string        `json:"tags"`
	Utensils  []string        `json:"utensils"`
	Allergens []string        `json:"allergens"`
	Nutrition []NutritionItem `json:"nutrition"`

	Ingredients1P []Ingredient `json:"ingredients_1p"`
	Steps         []Step       `json:"steps"`
}

type NutritionItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type Ingredient struct {
	Name  string `json:"name"`
	Qty1P string `json:"qty_1p"`
	Icon  string `json:"icon"`
}

type Step struct {
	Num      string   `json:"num"`
	Title    string   `json:"title"`
	Bullets  []string `json:"bullets"`
	Image    string   `json:"image"`
	ImageAlt string   `json:"image_alt"`
}

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

	Ingredients1P []Ingredient
	Steps         []Step
}

// If the scraper produced a truncated JSON file, close remaining braces/brackets.
func sanitizeJSON(b []byte) []byte {
	s := string(b)

	openCurly := 0
	openSquare := 0
	inString := false
	escaped := false

	for _, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		if r == '"' {
			inString = true
			continue
		}
		switch r {
		case '{':
			openCurly++
		case '}':
			if openCurly > 0 {
				openCurly--
			}
		case '[':
			openSquare++
		case ']':
			if openSquare > 0 {
				openSquare--
			}
		}
	}

	var out strings.Builder
	out.WriteString(s)
	for i := 0; i < openSquare; i++ {
		out.WriteString("]")
	}
	for i := 0; i < openCurly; i++ {
		out.WriteString("}")
	}
	out.WriteString("\n")
	return []byte(out.String())
}

var unitFromParens = regexp.MustCompile(`\(([^)]+)\)\s*$`)

func inferUnit(label string) string {
	// Prefer explicit units like "(kJ)" "(kcal)" at end of label.
	if m := unitFromParens.FindStringSubmatch(label); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	// Common HelloFresh nutrition cards.
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

// Render builds the pixel-perfect HTML page using the provided template bytes.
func Render(recipe RecipeJSON, tmpl []byte) ([]byte, error) {
	vm := ViewModel{
		Recipe: RecipeVM{
			Title:         firstNonEmpty(recipe.Title, recipe.RecipeName),
			RecipeName:    firstNonEmpty(recipe.RecipeName, recipe.Title),
			RecipeNameMin: recipe.RecipeNameMin,
			Description:   recipe.Description,
			URL:           recipe.URL,
			Image:         recipe.Image,

			PrepTime:   recipe.PrepTime,
			Difficulty: recipe.Difficulty,
			Origin:     recipe.Origin,

			Tags:      recipe.Tags,
			Utensils:  recipe.Utensils,
			Allergens: recipe.Allergens,
			Nutrition: toNutritionVM(recipe.Nutrition),

			Ingredients1P: recipe.Ingredients1P,
			Steps:         recipe.Steps,
		},
	}

	tpl, err := template.New("page").Option("missingkey=error").Parse(string(tmpl))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, vm); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseRecipeJSON sanitizes and unmarshals a JSON document into the expected struct.
func ParseRecipeJSON(raw []byte) (RecipeJSON, error) {
	raw = sanitizeJSON(raw)
	var r RecipeJSON
	if err := json.Unmarshal(raw, &r); err != nil {
		return RecipeJSON{}, err
	}
	return r, nil
}
