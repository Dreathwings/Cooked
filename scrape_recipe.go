// scrape_recipe.go
//
// Scrape a hfresh recipe page and write a JSON file.
//
// Install:
//   go mod init hfclone
//   go get github.com/PuerkitoBio/goquery
//
// Run:
//   go run scrape_recipe.go -url "https://hfresh.info/fr-FR/recipes/..." -out recipe.json

package main

import (
        "bytes"
        "encoding/json"
        "flag"
        "fmt"
        "io"
        "net/http"
        "os"
        "regexp"
        "sort"
        "strings"
        "time"

        "github.com/PuerkitoBio/goquery"
)

var (
        spaceRe   = regexp.MustCompile(`\s+`)
        numRe     = regexp.MustCompile(`^\d+$`)
        minutesRe = regexp.MustCompile(`^\s*\d+\s*min\s*$`)
)

func norm(s string) string {
        s = strings.TrimSpace(s)
        s = spaceRe.ReplaceAllString(s, " ")
        return s
}

func httpGetBytes(u string) ([]byte, error) {
        req, err := http.NewRequest(http.MethodGet, u, nil)
        if err != nil {
                return nil, err
        }
        req.Header.Set("User-Agent", "hfclone-scraper/1.3 (+terminal)")
        c := &http.Client{Timeout: 30 * time.Second}
        resp, err := c.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        if resp.StatusCode < 200 || resp.StatusCode > 299 {
                b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
                return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
        }
        return io.ReadAll(resp.Body)
}

func meta(doc *goquery.Document, key string) string {
        if v, ok := doc.Find(fmt.Sprintf(`meta[name="%s"]`, key)).Attr("content"); ok {
                return norm(v)
        }
        if v, ok := doc.Find(fmt.Sprintf(`meta[property="%s"]`, key)).Attr("content"); ok {
                return norm(v)
        }
        return ""
}

func unique(in []string) []string {
        seen := map[string]struct{}{}
        out := make([]string, 0, len(in))
        for _, s := range in {
                s = norm(s)
                if s == "" {
                        continue
                }
                if _, ok := seen[s]; ok {
                        continue
                }
                seen[s] = struct{}{}
                out = append(out, s)
        }
        return out
}

// -------------------- DATA MODEL (JSON) --------------------

type Ingredient struct {
        Name  string `json:"name"`
        Qty1P string `json:"qty_1p"`
        Icon  string `json:"icon"`
}

type NutritionItem struct {
        Label string `json:"label"`
        Value string `json:"value"`
}

type Step struct {
        Num      string   `json:"num"`
        Title    string   `json:"title"`
        Bullets  []string `json:"bullets"`
        Image    string   `json:"image"`
        ImageAlt string   `json:"image_alt"`
}

type RecipeJSON struct {
        Title         string `json:"title"`
        RecipeName    string `json:"recipe_name"`
        RecipeNameMin string `json:"recipe_name_min"`

        Description string `json:"description"`
        URL         string `json:"url"`
        Image       string `json:"image"`

        PrepTime   string `json:"prep_time"`
        Difficulty string `json:"difficulty"`
        Origin     string `json:"origin"`

        Tags      []string `json:"tags"`
        Utensils  []string `json:"utensils"`
        Allergens []string `json:"allergens"`

        Nutrition    []NutritionItem `json:"nutrition"`
        Ingredients1 []Ingredient    `json:"ingredients_1p"`
        Steps        []Step          `json:"steps"`
}

// -------------------- EXTRACTORS --------------------

// recipe_name_min: overlay text in the hero image
func extractRecipeNameMin(doc *goquery.Document) string {
        var res string

        doc.Find("div.absolute").EachWithBreak(func(_ int, d *goquery.Selection) bool {
                cls := d.AttrOr("class", "")
                if !strings.Contains(cls, "bottom-0") || !strings.Contains(cls, "left-0") || !strings.Contains(cls, "right-0") {
                        return true
                }
                p := d.Find("p").First()
                if p.Length() == 0 {
                        return true
                }
                t := norm(p.Text())
                if t != "" {
                        res = t
                        return false
                }
                return true
        })

        return res
}

// Difficulty / prep time / origin from the header facts bar
func extractHeaderFacts(doc *goquery.Document) (prepTime, difficulty, origin string) {
        var diffSpan *goquery.Selection

        doc.Find("span").EachWithBreak(func(_ int, s *goquery.Selection) bool {
                t := norm(s.Text())
                if strings.Contains(strings.ToLower(t), "difficult") {
                        diffSpan = s
                        return false
                }
                return true
        })
        if diffSpan == nil || diffSpan.Length() == 0 {
                // fallback: best effort scan
                doc.Find("span").Each(func(_ int, s *goquery.Selection) {
                        t := norm(s.Text())
                        lt := strings.ToLower(t)
                        if difficulty == "" && strings.Contains(lt, "difficult") {
                                difficulty = t
                        } else if prepTime == "" && minutesRe.MatchString(t) {
                                prepTime = t
                        }
                })
                return
        }

        outer := diffSpan.Parent().Parent()
        outer.Find("span").Each(func(_ int, s *goquery.Selection) {
                t := norm(s.Text())
                if t == "" {
                        return
                }
                lt := strings.ToLower(t)

                if strings.Contains(lt, "difficult") && difficulty == "" {
                        difficulty = t
                        return
                }
                if minutesRe.MatchString(t) && prepTime == "" {
                        prepTime = t
                        return
                }
                // origin: the remaining short label
                if origin == "" && !strings.Contains(t, ":") && !minutesRe.MatchString(t) && !strings.Contains(lt, "difficult") {
                        origin = t
                        return
                }
        })

        return
}

func extractPills(doc *goquery.Document, title string) []string {
        var res []string
        doc.Find("p").EachWithBreak(func(_ int, psel *goquery.Selection) bool {
                if strings.EqualFold(norm(psel.Text()), title) {
                        wrap := psel.Parent().Find("div.flex.flex-wrap.gap-1").First()
                        if wrap.Length() == 0 {
                                wrap = psel.NextAllFiltered("div").First()
                        }
                        if wrap.Length() == 0 {
                                wrap = psel.Parent().NextAllFiltered("div").First()
                        }
                        wrap.Children().Each(func(_ int, c *goquery.Selection) {
                                if t := norm(c.Text()); t != "" {
                                        res = append(res, t)
                                }
                        })
                        return false
                }
                return true
        })
        res = unique(res)
        sort.Strings(res)
        return res
}

func extractIngredients(doc *goquery.Document) []Ingredient {
        var out []Ingredient
        seen := map[string]struct{}{}

        doc.Find("div.flex.items-center.gap-3").Each(func(_ int, row *goquery.Selection) {
                im := row.Find("img[alt]").First()
                if im.Length() == 0 {
                        return
                }
                name := norm(im.AttrOr("alt", ""))
                if name == "" {
                        return
                }
                ps := row.Find("p")
                if ps.Length() < 2 {
                        return
                }
                qty := norm(ps.Last().Text())
                if qty == "" || !strings.ContainsAny(qty, "0123456789") {
                        return
                }
                if _, ok := seen[name]; ok {
                        return
                }
                seen[name] = struct{}{}

                out = append(out, Ingredient{
                        Name:  name,
                        Qty1P: qty,
                        Icon:  norm(im.AttrOr("src", "")),
                })
        })

        // keep stable order (optional)
        sort.SliceStable(out, func(i, j int) bool {
                return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
        })
        return out
}

// FIX: nutrition extraction must be scoped to the "Nutrition" section (not global data-flux-card)
func extractNutrition(doc *goquery.Document) []NutritionItem {
        // Find the heading "Nutrition" (div.font-medium), then grab the nearest grid below it
        var heading *goquery.Selection
        doc.Find("div.font-medium").EachWithBreak(func(_ int, s *goquery.Selection) bool {
                t := norm(s.Text())
                if strings.HasPrefix(t, "Nutrition") { // "Nutrition (per serving / 1 personne)"
                        heading = s
                        return false
                }
                return true
        })

        if heading == nil || heading.Length() == 0 {
                return nil
        }

        section := heading.Parent()
        grid := section.Find("div.grid").First()
        if grid.Length() == 0 {
                grid = heading.NextAllFiltered("div.grid").First()
        }
        if grid.Length() == 0 {
                return nil
        }

        var out []NutritionItem
        grid.Find(`div[data-flux-card]`).Each(func(_ int, card *goquery.Selection) {
                ps := card.Find("p")
                if ps.Length() < 2 {
                        return
                }
                val := norm(ps.First().Text())
                lbl := norm(ps.Last().Text())
                if val == "" || lbl == "" {
                        return
                }
                out = append(out, NutritionItem{Label: lbl, Value: val})
        })

        return out
}

func extractSteps(doc *goquery.Document) []Step {
        var steps []Step
        doc.Find("div.flex.gap-4").Each(func(_ int, block *goquery.Selection) {
                num := norm(block.Children().First().Text())
                if !numRe.MatchString(num) {
                        return
                }
                content := block.Children().Eq(1)
                if content.Length() == 0 {
                        return
                }

                img := content.Find("img").First()
                imgSrc := ""
                imgAlt := ""
                if img.Length() > 0 {
                        imgSrc = norm(img.AttrOr("src", ""))
                        imgAlt = norm(img.AttrOr("alt", ""))
                        if !strings.Contains(imgSrc, "/step-") {
                                // not a step image; ignore
                                imgSrc, imgAlt = "", ""
                        }
                }

                title := norm(content.Find("p").First().Text())

                var bullets []string
                content.Find("ul li").Each(func(_ int, li *goquery.Selection) {
                        if t := norm(li.Text()); t != "" {
                                bullets = append(bullets, t)
                        }
                })

                steps = append(steps, Step{
                        Num:      num,
                        Title:    title,
                        Bullets:  unique(bullets),
                        Image:    imgSrc,
                        ImageAlt: imgAlt,
                })
        })

        // sort by step number
        sort.SliceStable(steps, func(i, j int) bool { return steps[i].Num < steps[j].Num })
        return steps
}

// -------------------- MAIN --------------------

func main() {
        urlFlag := flag.String("url", "", "hfresh recipe URL")
        outFlag := flag.String("out", "recipe.json", "output JSON file")
        flag.Parse()

        if *urlFlag == "" {
                fmt.Fprintln(os.Stderr, "Error: -url is required")
                os.Exit(2)
        }

        raw, err := httpGetBytes(*urlFlag)
        if err != nil {
                fmt.Fprintln(os.Stderr, "Fetch error:", err)
                os.Exit(1)
        }

        doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
        if err != nil {
                fmt.Fprintln(os.Stderr, "Parse error:", err)
                os.Exit(1)
        }

        title := norm(doc.Find("title").First().Text())
        recipeName := meta(doc, "og:title")
        if recipeName == "" {
                recipeName = title
        }

        prepTime, difficulty, origin := extractHeaderFacts(doc)

        j := RecipeJSON{
                Title:         title,
                RecipeName:    recipeName,
                RecipeNameMin: extractRecipeNameMin(doc),

                Description: meta(doc, "description"),
                URL:         meta(doc, "og:url"),
                Image:       meta(doc, "og:image"),

                PrepTime:   prepTime,
                Difficulty: difficulty,
                Origin:     origin,

                Tags:      extractPills(doc, "Tags"),
                Utensils:  extractPills(doc, "Utensils"),
                Allergens: extractPills(doc, "Allergens"),

                Nutrition:    extractNutrition(doc),
                Ingredients1: extractIngredients(doc),
                Steps:        extractSteps(doc),
        }

        out, err := json.MarshalIndent(j, "", "  ")
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
                j.PrepTime, j.Difficulty, j.Origin, j.RecipeNameMin, len(j.Nutrition))
}
