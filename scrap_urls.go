package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func FetchRecipeURLs(ctx context.Context, base string, maxPage int, delay time.Duration) ([]string, error) {
	client := &http.Client{Timeout: 25 * time.Second}
	seen := make(map[string]struct{}, 10000)
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}

	for p := 1; p <= maxPage; p++ {
		pageURL := fmt.Sprintf("%s?page=%d", strings.TrimRight(base, "/"), p)
		added, finalBase, err := scrapeOne(ctx, client, pageURL, seen)
		if err != nil {
			log.Printf("[page %d] error: %v", p, err)
		} else {
			baseURL = finalBase
			if added == 0 && p > 1 {
				log.Printf("no recipes found on page %d -> stopping early", p)
				break
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	all := make([]string, 0, len(seen))
	for u := range seen {
		all = append(all, u)
	}
	sort.Strings(all)
	_ = baseURL
	return all, nil
}

func scrapeOne(ctx context.Context, client *http.Client, pageURL string, seen map[string]struct{}) (added int, effectiveBase *url.URL, err error) {
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
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || href == "" {
			return
		}
		if !strings.Contains(href, "/recipes/") {
			return
		}

		u, err := url.Parse(href)
		if err != nil {
			return
		}

		abs := effectiveBase.ResolveReference(u)
		if abs.Host != "hfresh.info" {
			return
		}

		finalURL := abs.String()
		if _, exists := seen[finalURL]; !exists {
			seen[finalURL] = struct{}{}
			added++
		}
	})
	return added, effectiveBase, nil
}
