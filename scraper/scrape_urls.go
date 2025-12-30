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
        baseStr := flag.String("base", "https://hfresh.info/fr-FR", "Page liste recettes (ex: https://hfresh.info/fr-FR>
        maxPage := flag.Int("max", 795, "Nombre max de pages à parcourir")
        delay := flag.Duration("delay", 300*time.Millisecond, "Délai entre requêtes")
        out := flag.String("out", "recipe_urls.txt", "Fichier de sortie")
        flag.Parse()

        baseURL, err := url.Parse(*baseStr)

^G Help        ^O Write Out   ^W Where Is    ^K Cut         ^T Execute     ^C Location    M-U Undo       M-A Set Mark
^X Exit        ^R Read File   ^\ Replace     ^U Paste       ^J Justify     ^/ Go To Line  M-E Redo       M-6 Copy
