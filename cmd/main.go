package main

import (
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
func generateURLs(entryURLs ...string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		seen := make(map[string]bool)
		var count int
		var generate func(url string)
		generate = func(url string) {
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
			for _, url := range extractURLs(string(body)) {
				if count >= maxURL {
					log.Println("stop generating URL due to exceeding max count")
					return
				}
				log.Printf("generate URL = %s", url)
				out <- url

				time.Sleep(delay)

				generate(url)
			}
		}
		for _, entryURL := range entryURLs {
			generate(entryURL)
		}
	}()
	return out
}

// Fan-out pattern
func fetchURL(urls <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for url := range urls {
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
		close(out)
	}()
	return out
}

// Fan-in pattern
func processResponses(responses []<-chan string) <-chan string {
	var wg sync.WaitGroup
	out := make(chan string)

	output := func(c <-chan string) {
		defer wg.Done()
		for resp := range c {
			out <- resp
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
func processHTML(html <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for h := range html {
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
			}
			processedHTML := fmt.Sprintf("processed HTML = %s", string(jsonStr))
			out <- processedHTML
		}
	}()
	return out
}

// Worker pool pattern
func workerPool(in <-chan string, numWorkers int) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for html := range in {
				processedHTML := html
				out <- processedHTML
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

	// Generate URLs from entry URLs
	urlChannel := generateURLs(entryURLs...)

	// Fetch HTML content from URLs
	numWorkers := runtime.NumCPU()
	fetchedChannels := make([]<-chan string, numWorkers)
	for i := 0; i < numWorkers; i++ {
		fetchedChannels[i] = fetchURL(urlChannel)
	}

	// Collect and merge HTML content from multiple channels
	processedHTML := processResponses(fetchedChannels)

	// Process HTML content
	processedData := processHTML(processedHTML)

	// Worker pool for processing HTML content
	numWorkerPool := 2
	finalResult := workerPool(processedData, numWorkerPool)

	// Output
	for res := range finalResult {
		log.Println(res)
	}
}
