package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Generator pattern
func generateURLs(urls ...string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for _, url := range urls {
			out <- url
		}
	}()
	return out
}

// Fan-out pattern
func fetchURL(url <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for u := range url {
			resp, err := http.Get(u)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			out <- string(body)
		}
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
			out <- "Processed HTML: " + h[:50] // Just for demonstration
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
			for url := range in {
				out <- url
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
	urls := []string{"https://example.com", "https://golang.org", "https://reddit.com"}

	// Generator pattern
	urlChannel := generateURLs(urls...)

	// Fan-out pattern
	numWorkers := 3
	fetchedChannels := make([]<-chan string, numWorkers)
	for i := 0; i < numWorkers; i++ {
		fetchedChannels[i] = fetchURL(urlChannel)
	}

	// Fan-in pattern
	processedHTML := processResponses(fetchedChannels)

	// Pipeline pattern
	processedData := processHTML(processedHTML)

	// Worker pool pattern
	numWorkerPool := 2
	finalResult := workerPool(processedData, numWorkerPool)

	for res := range finalResult {
		fmt.Println(res)
	}
}
