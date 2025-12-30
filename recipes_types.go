package main

type ScrapedIngredient struct {
	Name  string `json:"name"`
	Qty1P string `json:"qty_1p"`
	Icon  string `json:"icon"`
}

type NutritionItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type ScrapedStep struct {
	Num      string   `json:"num"`
	Title    string   `json:"title"`
	Bullets  []string `json:"bullets"`
	Image    string   `json:"image"`
	ImageAlt string   `json:"image_alt"`
}

type RecipeDocument struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	RecipeName    string              `json:"recipe_name"`
	RecipeNameMin string              `json:"recipe_name_min"`
	Description   string              `json:"description"`
	URL           string              `json:"url"`
	Image         string              `json:"image"`
	PrepTime      string              `json:"prep_time"`
	Difficulty    string              `json:"difficulty"`
	Origin        string              `json:"origin"`
	Tags          []string            `json:"tags"`
	Utensils      []string            `json:"utensils"`
	Allergens     []string            `json:"allergens"`
	Nutrition     []NutritionItem     `json:"nutrition"`
	Ingredients1P []ScrapedIngredient `json:"ingredients_1p"`
	Steps         []ScrapedStep       `json:"steps"`
}
