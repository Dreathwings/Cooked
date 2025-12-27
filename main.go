package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.gohtml
var templateFS embed.FS

type Recipe struct {
	ID          string
	Title       string
	Description string
	ImageURL    string
	SourceURL   string
	PrepTime    string
	CookTime    string
	TotalTime   string
	Difficulty  string
	Cuisine     string
	Tags        []string
	Ingredients []Ingredient
	Steps       []string
	Servings    int
	Weekly      bool
}

type Ingredient struct {
	Name     string
	Quantity string
}

type ShoppingEntry struct {
	RecipeID    string
	Title       string
	Servings    int
	Ingredients []Ingredient
}

type App struct {
	mu          sync.RWMutex
	recipes     []Recipe
	shopping    map[string]ShoppingEntry
	lastUpdated time.Time
	scraper     *Scraper
	templates   *template.Template
	refreshCh   chan struct{}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp()
	app.loadBuiltins()
	app.startBackgroundRefresh(ctx)
	app.enqueueRefresh()

	server := &http.Server{
		Addr:         ":3044",
		Handler:      app.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 20 * time.Second,
	}

	log.Printf("Serveur disponible sur http://localhost%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Échec du serveur : %v", err)
	}
}

func NewApp() *App {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.gohtml"))
	return &App{
		shopping:  make(map[string]ShoppingEntry),
		scraper:   NewScraper("https://hfresh.info/fr-FR"),
		templates: tmpl,
		refreshCh: make(chan struct{}, 1),
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.redirectHome)
	mux.HandleFunc("/recettes", a.recipesHandler)
	mux.HandleFunc("/recettes/", a.recipeDetailHandler)
	mux.HandleFunc("/semaine", a.weeklyHandler)
	mux.HandleFunc("/courses", a.shoppingHandler)
	mux.HandleFunc("/courses/ajouter", a.addToShoppingHandler)
	mux.HandleFunc("/courses/mettre-a-jour", a.updateShoppingHandler)
	mux.HandleFunc("/courses/supprimer", a.removeShoppingHandler)
	mux.HandleFunc("/refresh", a.refreshHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	return logRequests(mux)
}

func (a *App) redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/recettes", http.StatusFound)
}

func (a *App) recipesHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	filtered := filterRecipes(a.recipes, r.URL.Query())
	data := map[string]any{
		"Recipes":     filtered,
		"Query":       r.URL.Query().Get("q"),
		"Diet":        r.URL.Query().Get("diet"),
		"Difficulty":  r.URL.Query().Get("difficulty"),
		"Tag":         r.URL.Query().Get("tag"),
		"LastUpdated": a.lastUpdated,
		"Count":       len(filtered),
		"AllCount":    len(a.recipes),
		"Active":      "recettes",
	}
	a.render(w, "recipes.gohtml", data)
}

func (a *App) weeklyHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var weekly []Recipe
	for _, recipe := range a.recipes {
		if recipe.Weekly {
			weekly = append(weekly, recipe)
		}
	}
	data := map[string]any{
		"Recipes":     weekly,
		"LastUpdated": a.lastUpdated,
		"Count":       len(weekly),
		"Active":      "semaine",
	}
	a.render(w, "weekly.gohtml", data)
}

func (a *App) recipeDetailHandler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/recettes/") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/recettes/")
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, recipe := range a.recipes {
		if recipe.ID == id {
			data := map[string]any{
				"Recipe":      recipe,
				"LastUpdated": a.lastUpdated,
				"Active":      "recettes",
			}
			a.render(w, "detail.gohtml", data)
			return
		}
	}
	http.NotFound(w, r)
}

func (a *App) shoppingHandler(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var entries []ShoppingEntry
	for _, entry := range a.shopping {
		entries = append(entries, entry)
	}
	data := map[string]any{
		"Entries": entries,
		"Active":  "courses",
	}
	a.render(w, "shopping.gohtml", data)
}

func (a *App) addToShoppingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulaire invalide", http.StatusBadRequest)
		return
	}
	recipeID := r.FormValue("recipe_id")
	servings, _ := strconv.Atoi(r.FormValue("servings"))

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, recipe := range a.recipes {
		if recipe.ID == recipeID {
			entry := ShoppingEntry{
				RecipeID:    recipe.ID,
				Title:       recipe.Title,
				Servings:    servings,
				Ingredients: scaleIngredients(recipe.Ingredients, recipe.Servings, servings),
			}
			a.shopping[recipe.ID] = entry
			break
		}
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

func (a *App) updateShoppingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulaire invalide", http.StatusBadRequest)
		return
	}
	recipeID := r.FormValue("recipe_id")
	servings, _ := strconv.Atoi(r.FormValue("servings"))

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, recipe := range a.recipes {
		if recipe.ID == recipeID {
			entry, exists := a.shopping[recipeID]
			if exists {
				entry.Servings = servings
				entry.Ingredients = scaleIngredients(recipe.Ingredients, recipe.Servings, servings)
				a.shopping[recipeID] = entry
			}
			break
		}
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

func (a *App) removeShoppingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formulaire invalide", http.StatusBadRequest)
		return
	}
	recipeID := r.FormValue("recipe_id")
	a.mu.Lock()
	delete(a.shopping, recipeID)
	a.mu.Unlock()
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

func (a *App) refreshHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/recettes", http.StatusSeeOther)
		return
	}
	a.enqueueRefresh()
	http.Redirect(w, r, "/recettes?refresh=en-cours", http.StatusSeeOther)
}

func (a *App) startBackgroundRefresh(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-a.refreshCh:
				// Utilise un contexte indépendant pour éviter d'annuler le scraping
				// en cas de timeout trop court côté requête HTTP initiale.
				refreshCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := a.Refresh(refreshCtx); err != nil {
					log.Printf("Actualisation échouée : %v", err)
				}
				cancel()
			}
		}
	}()
}

func (a *App) enqueueRefresh() {
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

func (a *App) Refresh(ctx context.Context) error {
	recipes, err := a.scraper.FetchAll(ctx)
	if err != nil || len(recipes) == 0 {
		recipes = builtinRecipes()
		err = fmt.Errorf("retombé sur les recettes intégrées : %w", err)
	}
	a.mu.Lock()
	a.recipes = recipes
	a.lastUpdated = time.Now()
	a.mu.Unlock()
	return err
}

func (a *App) loadBuiltins() {
	a.mu.Lock()
	a.recipes = builtinRecipes()
	a.lastUpdated = time.Now()
	a.mu.Unlock()
}

func (a *App) render(w http.ResponseWriter, name string, data map[string]any) {
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Erreur de rendu", http.StatusInternalServerError)
		log.Printf("erreur template %s : %v", name, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("erreur d'écriture : %v", err)
	}
}

func filterRecipes(recipes []Recipe, params url.Values) []Recipe {
	query := strings.ToLower(strings.TrimSpace(params.Get("q")))
	diet := strings.ToLower(strings.TrimSpace(params.Get("diet")))
	diff := strings.ToLower(strings.TrimSpace(params.Get("difficulty")))
	tag := strings.ToLower(strings.TrimSpace(params.Get("tag")))

	var filtered []Recipe
	for _, recipe := range recipes {
		if query != "" && !strings.Contains(strings.ToLower(recipe.Title), query) && !strings.Contains(strings.ToLower(recipe.Description), query) {
			continue
		}
		if diet != "" && !containsFold(recipe.Tags, diet) {
			continue
		}
		if diff != "" && !strings.EqualFold(recipe.Difficulty, diff) {
			continue
		}
		if tag != "" && !containsFold(recipe.Tags, tag) {
			continue
		}
		filtered = append(filtered, recipe)
	}
	return filtered
}

func containsFold(list []string, q string) bool {
	for _, v := range list {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

// Scraper logic

type Scraper struct {
	baseURL string
	client  *http.Client
	ua      string
}

func NewScraper(baseURL string) *Scraper {
	return &Scraper{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		ua: "CookedScraper/1.0 (+https://hfresh.info)",
	}
}

func (s *Scraper) FetchAll(ctx context.Context) ([]Recipe, error) {
	urls, err := s.gatherRecipeURLs(ctx)
	if err != nil {
		return nil, err
	}
	var recipes []Recipe
	for _, u := range urls {
		recipe, err := s.scrapeRecipe(ctx, u)
		if err != nil {
			log.Printf("erreur de scraping %s : %v", u, err)
			continue
		}
		if recipe.ID == "" {
			recipe.ID = urlToID(u)
		}
		if recipe.SourceURL == "" {
			recipe.SourceURL = u
		}
		recipes = append(recipes, recipe)
		if len(recipes) >= 48 {
			break
		}
	}
	recipes = markWeekly(recipes)
	return recipes, nil
}

func (s *Scraper) gatherRecipeURLs(ctx context.Context) ([]string, error) {
	sitemapURL := s.baseURL + "/sitemap.xml"
	req, err := s.newRequest(ctx, http.MethodGet, sitemapURL)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		type loc struct {
			Loc string `xml:"loc"`
		}
		type urlset struct {
			URLs []loc `xml:"url"`
		}
		var sm urlset
		if err := xml.NewDecoder(resp.Body).Decode(&sm); err == nil {
			var urls []string
			for _, u := range sm.URLs {
				if strings.Contains(u.Loc, "/recipes") || strings.Contains(u.Loc, "/recettes") {
					urls = append(urls, u.Loc)
				}
			}
			if len(urls) > 0 {
				return urls, nil
			}
		}
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Fallback: fetch home page and extract recipe links.
	homeReq, err := s.newRequest(ctx, http.MethodGet, s.baseURL)
	if err != nil {
		return nil, err
	}
	homeResp, err := s.client.Do(homeReq)
	if err != nil {
		return nil, err
	}
	defer homeResp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(homeResp.Body); err != nil {
		return nil, err
	}
	body := buf.String()
	urls := extractRecipeLinks(body, s.baseURL)

	// Pagination : parcourir les pages ?page=2,3,... tant que des liens sont trouvés.
	for page := 2; page <= 12; page++ {
		pageURL := fmt.Sprintf("%s?page=%d", s.baseURL, page)
		req, err := s.newRequest(ctx, http.MethodGet, pageURL)
		if err != nil {
			break
		}
		resp, err := s.client.Do(req)
		if err != nil {
			break
		}
		pageBuf := new(bytes.Buffer)
		if _, err := pageBuf.ReadFrom(resp.Body); err != nil {
			resp.Body.Close()
			break
		}
		resp.Body.Close()
		pageLinks := extractRecipeLinks(pageBuf.String(), s.baseURL)
		if len(pageLinks) == 0 {
			break
		}
		urls = append(urls, pageLinks...)
	}

	return uniqueStrings(urls), nil
}

func (s *Scraper) scrapeRecipe(ctx context.Context, recipeURL string) (Recipe, error) {
	req, err := s.newRequest(ctx, http.MethodGet, recipeURL)
	if err != nil {
		return Recipe{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Recipe{}, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return Recipe{}, err
	}
	body := buf.String()
	if recipe, ok := parseJSONLDRecipe(body); ok {
		recipe.SourceURL = recipeURL
		return recipe, nil
	}
	if recipe, ok := parseFallbackRecipe(body, recipeURL); ok {
		return recipe, nil
	}
	return Recipe{}, fmt.Errorf("aucune donnée Recipe détectée")
}

func (s *Scraper) newRequest(ctx context.Context, method, target string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	if s.ua != "" {
		req.Header.Set("User-Agent", s.ua)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9,en;q=0.8")
	return req, nil
}

func extractRecipeLinks(body, base string) []string {
	var links []string
	re := regexp.MustCompile(`href="([^"]+)"`)
	matches := re.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		link := m[1]
		lower := strings.ToLower(link)
		if strings.Contains(lower, "/recettes/") || strings.Contains(lower, "/recipes/") {
			if strings.HasPrefix(link, "http") {
				links = append(links, link)
			} else if strings.HasPrefix(link, "/") {
				links = append(links, base+link)
			}
		}
	}
	return uniqueStrings(links)
}

func parseJSONLDRecipe(body string) (Recipe, bool) {
	scripts := append(findJSONLDBlocks(body), findAdditionalJSONBlocks(body)...)
	for _, block := range scripts {
		var payload any
		normalized := normalizeJSONBlock(block)
		if normalized == "" {
			continue
		}
		if err := json.Unmarshal([]byte(normalized), &payload); err != nil {
			continue
		}
		if recipe, ok := findRecipeInAny(payload); ok {
			return recipe, true
		}
	}
	return Recipe{}, false
}

func findJSONLDBlocks(body string) []string {
	re := regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	matches := re.FindAllStringSubmatch(body, -1)
	var blocks []string
	for _, match := range matches {
		if len(match) > 1 {
			blocks = append(blocks, strings.TrimSpace(match[1]))
		}
	}
	return blocks
}

func findAdditionalJSONBlocks(body string) []string {
	re := regexp.MustCompile(`(?is)<script[^>]*type=["']application/json["'][^>]*>(.*?)</script>`)
	matches := re.FindAllStringSubmatch(body, -1)
	var blocks []string
	for _, match := range matches {
		if len(match) > 1 {
			blocks = append(blocks, strings.TrimSpace(match[1]))
		}
	}
	return blocks
}

func normalizeJSONBlock(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "window.__NEXT_DATA__ =")
	content = strings.TrimPrefix(content, "window.__INITIAL_STATE__=")
	content = strings.TrimSuffix(content, ";")
	content = strings.TrimSpace(content)
	start := strings.IndexAny(content, "{[")
	if start == -1 {
		return ""
	}
	end := strings.LastIndexAny(content, "}]")
	if end == -1 || end < start {
		return ""
	}
	return strings.TrimSpace(content[start : end+1])
}

func findRecipeInAny(data any) (Recipe, bool) {
	switch v := data.(type) {
	case map[string]any:
		if recipe, ok := decodeRecipeMap(v); ok {
			return recipe, true
		}
		if graph, ok := v["@graph"]; ok {
			if recipe, ok := findRecipeInAny(graph); ok {
				return recipe, true
			}
		}
		for _, nested := range v {
			if recipe, ok := findRecipeInAny(nested); ok {
				return recipe, true
			}
		}
	case []any:
		for _, item := range v {
			if recipe, ok := findRecipeInAny(item); ok {
				return recipe, true
			}
		}
	}
	return Recipe{}, false
}

func parseFallbackRecipe(body, recipeURL string) (Recipe, bool) {
	title := extractMeta(body, []string{`property=["']og:title["']`, `name=["']title["']`})
	if title == "" {
		title = extractTitleTag(body)
	}
	if title == "" {
		return Recipe{}, false
	}
	desc := extractMeta(body, []string{`property=["']og:description["']`, `name=["']description["']`})
	image := extractMeta(body, []string{`property=["']og:image["']`, `name=["']image["']`})
	return Recipe{
		ID:          urlToID(recipeURL),
		Title:       htmlUnescape(title),
		Description: htmlUnescape(desc),
		ImageURL:    image,
		SourceURL:   recipeURL,
	}, true
}

func extractMeta(body string, patterns []string) string {
	for _, p := range patterns {
		re := regexp.MustCompile(`(?is)<meta[^>]+` + p + `[^>]*content=["']([^"']+)["'][^>]*>`)
		if match := re.FindStringSubmatch(body); len(match) > 1 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func extractTitleTag(body string) string {
	re := regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	if match := re.FindStringSubmatch(body); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func htmlUnescape(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

func decodeRecipeMap(m map[string]any) (Recipe, bool) {
	var types []string
	switch t := m["@type"].(type) {
	case string:
		types = []string{t}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok {
				types = append(types, s)
			}
		}
	}
	isRecipe := false
	for _, t := range types {
		if strings.EqualFold(t, "Recipe") {
			isRecipe = true
			break
		}
	}
	if !isRecipe {
		return Recipe{}, false
	}

	recipe := Recipe{
		Title:       stringValue(m["name"]),
		Description: stringValue(m["description"]),
		ImageURL:    firstImage(m["image"]),
		PrepTime:    stringValue(m["prepTime"]),
		CookTime:    stringValue(m["cookTime"]),
		TotalTime:   stringValue(m["totalTime"]),
		Difficulty:  stringValue(m["recipeCategory"]),
		Cuisine:     stringValue(m["recipeCuisine"]),
		Servings:    parseServings(m["recipeYield"]),
		Tags:        splitKeywords(stringValue(m["keywords"])),
		Ingredients: parseIngredients(m["recipeIngredient"]),
		Steps:       parseInstructions(m["recipeInstructions"]),
	}

	if recipe.ID == "" {
		recipe.ID = urlToID(stringValue(m["url"]))
	}
	return recipe, true
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func firstString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				return s
			}
		}
	}
	return ""
}

func firstImage(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if u := stringValue(val["url"]); u != "" {
			return u
		}
		if u := stringValue(val["@id"]); u != "" {
			return u
		}
	case []any:
		for _, item := range val {
			if u := firstImage(item); u != "" {
				return u
			}
		}
	}
	return ""
}

func parseIngredients(v any) []Ingredient {
	var ingredients []Ingredient
	switch val := v.(type) {
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				ingredients = append(ingredients, Ingredient{Name: s})
				continue
			}
			if m, ok := item.(map[string]any); ok {
				name := stringValue(m["item"])
				if name == "" {
					name = stringValue(m["name"])
				}
				if name == "" {
					name = stringValue(m["text"])
				}
				if name != "" {
					ingredients = append(ingredients, Ingredient{Name: name})
				}
			}
		}
	case []string:
		for _, s := range val {
			ingredients = append(ingredients, Ingredient{Name: s})
		}
	}
	return ingredients
}

func parseInstructions(v any) []string {
	var steps []string
	switch val := v.(type) {
	case []any:
		for _, item := range val {
			switch inst := item.(type) {
			case string:
				steps = append(steps, inst)
			case map[string]any:
				if nested, ok := inst["itemListElement"]; ok {
					steps = append(steps, parseInstructions(nested)...)
					continue
				}
				if txt := stringValue(inst["text"]); txt != "" {
					steps = append(steps, txt)
				}
			}
		}
	case string:
		parts := strings.Split(val, ".")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				steps = append(steps, p)
			}
		}
	}
	return steps
}

func parseServings(v any) int {
	switch val := v.(type) {
	case string:
		if n, err := strconv.Atoi(strings.Fields(val)[0]); err == nil {
			return n
		}
	case float64:
		return int(val)
	case map[string]any:
		if raw, ok := val["value"]; ok {
			if n := parseServings(raw); n != 0 {
				return n
			}
		}
	}
	return 2
}

func splitKeywords(input string) []string {
	if input == "" {
		return nil
	}
	var tags []string
	for _, t := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ';' }) {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func urlToID(u string) string {
	u = strings.TrimSuffix(u, "/")
	parts := strings.Split(u, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range in {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func markWeekly(recipes []Recipe) []Recipe {
	for i := range recipes {
		if i < 8 {
			recipes[i].Weekly = true
		}
	}
	return recipes
}

func scaleIngredients(ingredients []Ingredient, baseServings, requested int) []Ingredient {
	if baseServings == 0 || requested == 0 {
		return ingredients
	}
	scale := float64(requested) / float64(baseServings)
	scaled := make([]Ingredient, len(ingredients))
	for i, ing := range ingredients {
		scaled[i] = Ingredient{
			Name:     ing.Name,
			Quantity: scaleQuantity(ing.Quantity, scale),
		}
	}
	return scaled
}

func scaleQuantity(quantity string, scale float64) string {
	if quantity == "" {
		return quantity
	}
	fields := strings.Fields(quantity)
	if len(fields) == 0 {
		return quantity
	}
	val, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", "."), 64)
	if err != nil {
		return quantity
	}
	val *= scale
	fields[0] = formatNumber(val)
	return strings.Join(fields, " ")
}

func formatNumber(n float64) string {
	if math.Abs(n-math.Round(n)) < 0.001 {
		return strconv.Itoa(int(math.Round(n)))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", n), "0"), ".")
}

func builtinRecipes() []Recipe {
	return []Recipe{
		{
			ID:          "lasagnes-vegetariennes",
			Title:       "Lasagnes végétariennes au pesto",
			Description: "Des couches généreuses de légumes rôtis, de pesto et de mozzarella fondante.",
			ImageURL:    "https://images.unsplash.com/photo-1604908177650-0ac1c9bb6467?auto=format&fit=crop&w=1200&q=80",
			SourceURL:   "https://hfresh.info/fr-FR/recettes/lasagnes-vegetariennes",
			PrepTime:    "20 min",
			CookTime:    "35 min",
			TotalTime:   "55 min",
			Difficulty:  "Facile",
			Cuisine:     "Italienne",
			Tags:        []string{"Végétarien", "Four", "Fromage"},
			Servings:    4,
			Weekly:      true,
			Ingredients: []Ingredient{
				{Name: "Feuilles de lasagnes fraîches", Quantity: "400 g"},
				{Name: "Courgette", Quantity: "2"},
				{Name: "Pesto basilic", Quantity: "120 g"},
				{Name: "Mozzarella", Quantity: "200 g"},
				{Name: "Tomates cerises", Quantity: "200 g"},
				{Name: "Parmesan râpé", Quantity: "50 g"},
			},
			Steps: []string{
				"Préchauffez le four à 200°C et coupez les légumes en fines tranches.",
				"Faites rôtir les courgettes 10 minutes avec un filet d'huile d'olive.",
				"Montez les lasagnes en alternant pâte, légumes, pesto et mozzarella.",
				"Terminez par du parmesan et enfournez 25 minutes jusqu'à coloration dorée.",
			},
		},
		{
			ID:          "curry-cremeux-poulet",
			Title:       "Curry crémeux de poulet coco",
			Description: "Poulet fondant, sauce coco parfumée au citron vert et gingembre.",
			ImageURL:    "https://images.unsplash.com/photo-1559050019-6b509a68e480?auto=format&fit=crop&w=1200&q=80",
			SourceURL:   "https://hfresh.info/fr-FR/recettes/curry-cremeux-poulet",
			PrepTime:    "15 min",
			CookTime:    "25 min",
			TotalTime:   "40 min",
			Difficulty:  "Moyen",
			Cuisine:     "Asiatique",
			Tags:        []string{"Poulet", "Sans lactose", "Rapide"},
			Servings:    3,
			Weekly:      true,
			Ingredients: []Ingredient{
				{Name: "Blancs de poulet", Quantity: "400 g"},
				{Name: "Lait de coco", Quantity: "250 ml"},
				{Name: "Pâte de curry rouge", Quantity: "2 c. à soupe"},
				{Name: "Gingembre frais", Quantity: "20 g"},
				{Name: "Citron vert", Quantity: "1"},
				{Name: "Riz jasmin", Quantity: "200 g"},
			},
			Steps: []string{
				"Faites revenir le poulet en dés avec le gingembre râpé.",
				"Ajoutez la pâte de curry, déglacez avec le lait de coco et laissez mijoter.",
				"Parfumez de jus de citron vert et servez avec le riz jasmin cuit.",
			},
		},
		{
			ID:          "tarte-tatin-tomates",
			Title:       "Tarte tatin aux tomates confites",
			Description: "Une tatin salée caramélisée, relevée de thym et de feta émiettée.",
			ImageURL:    "https://images.unsplash.com/photo-1506368249639-73a05d6f6488?auto=format&fit=crop&w=1200&q=80",
			SourceURL:   "https://hfresh.info/fr-FR/recettes/tarte-tatin-tomates",
			PrepTime:    "10 min",
			CookTime:    "30 min",
			TotalTime:   "40 min",
			Difficulty:  "Facile",
			Cuisine:     "Bistrot",
			Tags:        []string{"Végétarien", "Tomate", "Express"},
			Servings:    4,
			Weekly:      false,
			Ingredients: []Ingredient{
				{Name: "Tomates cerises", Quantity: "500 g"},
				{Name: "Pâte feuilletée", Quantity: "1"},
				{Name: "Sucre brun", Quantity: "2 c. à soupe"},
				{Name: "Vinaigre balsamique", Quantity: "1 c. à soupe"},
				{Name: "Feta", Quantity: "80 g"},
				{Name: "Thym frais", Quantity: "Quelques brins"},
			},
			Steps: []string{
				"Caramélisez le sucre avec le balsamique dans une poêle allant au four.",
				"Ajoutez les tomates, laissez confire 5 minutes, puis recouvrez de pâte.",
				"Enfournez 25 minutes à 190°C, retournez et parsemez de feta et de thym.",
			},
		},
	}
}
