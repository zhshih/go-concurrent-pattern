package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/temoto/robotstxt"

	"github.com/PuerkitoBio/goquery"
)

const (
	maxURL = 1000
	delay  = 300 * time.Millisecond
)

type Article struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

func extractURLs(html string) []string {
	re := regexp.MustCompile(`href=["'](https?://[^"']+)["']`)
	matches := re.FindAllStringSubmatch(html, -1)
	urls := make([]string, len(matches))
	for i, match := range matches {
		urls[i] = match[1]
	}
	return urls
}

// Generator pattern
func generateURLs(ctx context.Context, entryURLs ...string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		seen := make(map[string]bool)
		var count int
		var generate func(ctx context.Context, url string)
		generate = func(ctx context.Context, url string) {
			if count >= maxURL {
				return
			}
			if seen[url] {
				return
			}
			seen[url] = true
			count++

			resp, err := http.Get(url + "/robots.txt")
			if err != nil {
				log.Println("error fetching robots.txt:", err)
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Println("error reading robots.txt:", err)
				return
			}
			robots, err := robotstxt.FromBytes(data)
			if err != nil {
				log.Println("error parsing robots.txt:", err)
				return
			}
			if !robots.TestAgent(url, "Test123") {
				log.Println("URL disallowed by robots.txt:", url)
				return
			}

			resp, err = http.Get(url)
			if err != nil {
				log.Println("error fetching URL:", err)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Println("error reading body:", err)
				return
			}
			for _, u := range extractURLs(string(body)) {
				if count >= maxURL {
					log.Println("stop generating URL due to exceeding max count")
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- u:
					log.Printf("generate URL = %s", u)
					time.Sleep(delay)
					go generate(ctx, u)
				}
			}
		}
		for _, entryURL := range entryURLs {
			generate(ctx, entryURL)
		}
	}()
	return out
}

// Fan-out pattern
func fetchURL(ctx context.Context, urls <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for url := range urls {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("fetch URL = %s", url)
				resp, err := http.Get(url)
				if err != nil {
					log.Println("error fetching URL:", err)
					continue
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					log.Println("error reading body:", err)
					continue
				}
				out <- string(body)
			}
		}
	}()
	return out
}

// Fan-in pattern
func processResponses(ctx context.Context, responses []<-chan string) <-chan string {
	var wg sync.WaitGroup
	out := make(chan string)

	output := func(c <-chan string) {
		defer wg.Done()
		for resp := range c {
			select {
			case <-ctx.Done():
				return
			default:
				out <- resp
			}
		}
	}

	wg.Add(len(responses))
	for _, c := range responses {
		go output(c)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// Pipeline pattern
func processHTML(ctx context.Context, html <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case h, ok := <-html:
				if !ok {
					return
				}
				doc, err := goquery.NewDocumentFromReader(strings.NewReader(h))
				if err != nil {
					log.Println("error parsing HTML:", err)
					continue
				}

				title := doc.Find("h1").Text()
				author := doc.Find(".author").Text()
				date := doc.Find(".date").Text()
				category := doc.Find(".category").Text()
				content := doc.Find(".content").Text()
				if len(content) > 50 {
					content = content[:50] + "...(remaining content has been omitted for brevity)"
				}
				article := Article{
					Title:    title,
					Author:   author,
					Date:     date,
					Category: category,
					Content:  content,
				}
				jsonStr, err := json.Marshal(article)
				if err != nil {
					log.Println("error marshal article:", err)
					continue
				}
				processedHTML := fmt.Sprintf("processed HTML = %s", string(jsonStr))
				out <- processedHTML
			}
		}
	}()
	return out
}

// Worker pool pattern
func workerPool(ctx context.Context, in <-chan string, numWorkers int) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case html, ok := <-in:
					if !ok {
						return
					}
					processedHTML := html
					out <- processedHTML
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	entryURLs := []string{"https://en.wikipedia.org/wiki/Main_Page"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Generate URLs from entry URLs
	urlChannel := generateURLs(ctx, entryURLs...)

	// Fetch HTML content from URLs
	numWorkers := runtime.NumCPU()
	fetchedChannels := make([]<-chan string, numWorkers)
	for i := 0; i < numWorkers; i++ {
		fetchedChannels[i] = fetchURL(ctx, urlChannel)
	}

	// Collect and merge HTML content from multiple channels
	processedHTML := processResponses(ctx, fetchedChannels)

	// Process HTML content
	processedData := processHTML(ctx, processedHTML)

	// Worker pool for processing HTML content
	numWorkerPool := 2
	finalResult := workerPool(ctx, processedData, numWorkerPool)

	// Output
	for res := range finalResult {
		log.Println(res)
	}
}
