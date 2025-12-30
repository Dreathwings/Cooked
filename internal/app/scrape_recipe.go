package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func httpGetBytes(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hfclone-scraper/1.3 (+server)")
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
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

func extractIngredients(doc *goquery.Document) []ScrapedIngredient {
	var out []ScrapedIngredient
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

		out = append(out, ScrapedIngredient{
			Name:  name,
			Qty1P: qty,
			Icon:  norm(im.AttrOr("src", "")),
		})
	})

	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func extractNutrition(doc *goquery.Document) []NutritionItem {
	var heading *goquery.Selection
	doc.Find("div.font-medium").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t := norm(s.Text())
		if strings.HasPrefix(t, "Nutrition") {
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

func extractSteps(doc *goquery.Document) []ScrapedStep {
	var steps []ScrapedStep
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

		steps = append(steps, ScrapedStep{
			Num:      num,
			Title:    title,
			Bullets:  unique(bullets),
			Image:    imgSrc,
			ImageAlt: imgAlt,
		})
	})

	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Num < steps[j].Num })
	return steps
}

func ScrapeRecipe(ctx context.Context, recipeURL string) (RecipeDocument, error) {
	raw, err := httpGetBytes(ctx, recipeURL)
	if err != nil {
		return RecipeDocument{}, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return RecipeDocument{}, err
	}

	title := norm(doc.Find("title").First().Text())
	recipeName := meta(doc, "og:title")
	if recipeName == "" {
		recipeName = title
	}

	prepTime, difficulty, origin := extractHeaderFacts(doc)

	r := RecipeDocument{
		ID:            urlToID(recipeURL),
		Title:         title,
		RecipeName:    recipeName,
		RecipeNameMin: extractRecipeNameMin(doc),
		Description:   meta(doc, "description"),
		URL:           meta(doc, "og:url"),
		Image:         meta(doc, "og:image"),
		PrepTime:      prepTime,
		Difficulty:    difficulty,
		Origin:        origin,
		Tags:          extractPills(doc, "Tags"),
		Utensils:      extractPills(doc, "Utensils"),
		Allergens:     extractPills(doc, "Allergens"),
		Nutrition:     extractNutrition(doc),
		Ingredients1P: extractIngredients(doc),
		Steps:         extractSteps(doc),
	}

	if r.URL == "" {
		r.URL = recipeURL
	}

	return r, nil
}

func marshalRecipe(r RecipeDocument) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
