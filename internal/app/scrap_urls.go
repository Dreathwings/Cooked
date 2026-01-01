package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const debugScrapURLs = true

func FetchRecipeURLs(ctx context.Context, base string, delay time.Duration) ([]string, error) {
	client := &http.Client{Timeout: 25 * time.Second}

	seen := make(map[string]struct{}, 10000)
	ordered := make([]string, 0, 10000)

	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}

	if debugScrapURLs {
		log.Printf("[SCRAP] start base=%s delay=%s timeout=%s",
			baseURL.String(), delay, client.Timeout)
	}

	page := 1

	for {
		pageURL := fmt.Sprintf("%s?page=%d", strings.TrimRight(base, "/"), page)

		start := time.Now()
		added, finalBase, err := scrapeOne(ctx, client, pageURL, seen, &ordered)
		elapsed := time.Since(start)

		if err != nil {
			log.Printf("[SCRAP][page %d] ERROR after %s: %v", page, elapsed, err)
		} else {
			baseURL = finalBase
			log.Printf("[SCRAP][page %d] OK %s | +%d urls | total=%d",
				page, elapsed, added, len(ordered))

			// 🔑 arrêt logique : plus aucune nouvelle recette
			if added == 0 && page > 1 {
				log.Printf("[SCRAP] stop: no new recipes found on page %d", page)
				break
			}
		}

		select {
		case <-ctx.Done():
			log.Printf("[SCRAP] cancelled: %v", ctx.Err())
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		page++
	}

	if debugScrapURLs {
		log.Printf("[SCRAP] done: %d unique recipe URLs collected", len(ordered))
	}

	_ = baseURL
	return ordered, nil
}

func scrapeOne(
	ctx context.Context,
	client *http.Client,
	pageURL string,
	seen map[string]struct{},
	ordered *[]string,
) (added int, effectiveBase *url.URL, err error) {

	if debugScrapURLs {
		log.Printf("[SCRAP] fetch %s", pageURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; hfresh-scraper/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	effectiveBase = resp.Request.URL

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return 0, effectiveBase, err
	}

	added = 0
	candidate := 0
	dup := 0
	skippedHost := 0
	skippedParse := 0

	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		if !strings.Contains(href, "/recipes/") {
			return
		}

		candidate++

		u, err := url.Parse(href)
		if err != nil {
			skippedParse++
			if debugScrapURLs {
				log.Printf("[SCRAP][skip] invalid url: %q", href)
			}
			return
		}

		abs := effectiveBase.ResolveReference(u)
		host := strings.TrimPrefix(abs.Host, "www.")
		if host != "hfresh.info" {
			skippedHost++
			if debugScrapURLs {
				log.Printf("[SCRAP][skip] foreign host: %s", abs.String())
			}
			return
		}

		finalURL := abs.String()
		if _, exists := seen[finalURL]; exists {
			dup++
			if debugScrapURLs {
				log.Printf("[SCRAP][dup] %s", finalURL)
			}
			return
		}

		seen[finalURL] = struct{}{}
		*ordered = append(*ordered, finalURL)
		added++

		if debugScrapURLs {
			log.Printf("[SCRAP][add #%d] %s", len(*ordered), finalURL)
		}
	})

	if debugScrapURLs {
		log.Printf(
			"[SCRAP] page summary: candidates=%d added=%d dup=%d skippedHost=%d skippedParse=%d",
			candidate, added, dup, skippedHost, skippedParse,
		)
	}

	return added, effectiveBase, nil
}
