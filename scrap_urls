// scrap_url.go
package main

import (
        "bufio"
        "flag"
        "fmt"
        "log"
        "net/http"
        "net/url"
        "os"
        "sort"
        "strings"
        "time"

        "github.com/PuerkitoBio/goquery"
)

func main() {
        baseStr := flag.String("base", "https://hfresh.info/fr-FR", "Page liste recettes (ex: https://hfresh.info/fr-FR ou https://hfresh.info/fr-fr)")
        maxPage := flag.Int("max", 795, "Nombre max de pages à parcourir")
        delay := flag.Duration("delay", 300*time.Millisecond, "Délai entre requêtes")
        out := flag.String("out", "recipe_urls.txt", "Fichier de sortie")
        flag.Parse()

        baseURL, err := url.Parse(*baseStr)
        if err != nil {
                log.Fatalf("bad base url: %v", err)
        }

        client := &http.Client{Timeout: 25 * time.Second}

        seen := make(map[string]struct{}, 10000)

        for p := 1; p <= *maxPage; p++ {
                pageURL := fmt.Sprintf("%s?page=%d", strings.TrimRight(*baseStr, "/"), p)

                added, finalBase, err := scrapeOne(client, pageURL, seen)
                if err != nil {
                        log.Printf("[page %d] error: %v", p, err)
                } else {
                        log.Printf("[page %d] +%d", p, added)
                        // base effective après redirections (ex: /fr-FR -> /fr-fr)
                        baseURL = finalBase

                        if added == 0 && p > 1 {
                                log.Printf("no recipes found on page %d -> stopping early", p)
                                break
                        }
                }

                time.Sleep(*delay)
        }

        all := make([]string, 0, len(seen))
        for u := range seen {
                all = append(all, u)
        }
        sort.Strings(all)

        if err := writeLines(*out, all); err != nil {
                log.Fatalf("write: %v", err)
        }
        log.Printf("done: %d urls -> %s", len(all), *out)

        _ = baseURL // juste pour éviter lint si tu ajoutes des logs plus tard
}

func scrapeOne(client *http.Client, pageURL string, seen map[string]struct{}) (added int, effectiveBase *url.URL, err error) {
        req, err := http.NewRequest("GET", pageURL, nil)
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

        // URL finale après redirections
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

        // uniquement les pages recette
        if !strings.Contains(href, "/recipes/") {
                return
        }

        u, err := url.Parse(href)
        if err != nil {
                return
        }

        abs := effectiveBase.ResolveReference(u)

        // 🔒 filtre strict sur le domaine
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

func writeLines(path string, lines []string) error {
        f, err := os.Create(path)
        if err != nil {
                return err
        }
        defer f.Close()

        w := bufio.NewWriter(f)
        for _, line := range lines {
                if _, err := w.WriteString(line + "\n"); err != nil {
                        return err
                }
        }
        return w.Flush()
}
