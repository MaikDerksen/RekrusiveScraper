package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

const (
	maxRecursion  = 20
	maxGoroutines = 10 // Limit the number of concurrent goroutines
)

var (
	visitedUrls = make(map[string]bool)
	mu          sync.Mutex
	semaphore   = make(chan struct{}, maxGoroutines) // Semaphore for controlling concurrency
)

// getDomainFolder creates a base folder named after the domain for each URL.
func getDomainFolder(urlStr string) (string, error) {
	parsedUrl, err := publicsuffix.EffectiveTLDPlusOne(urlStr)
	if err != nil {
		return "", err
	}
	domain := regexp.MustCompile(`[^\w]+`).ReplaceAllString(parsedUrl, "_")
	baseFolder := filepath.Join("data", domain)
	err = os.MkdirAll(baseFolder, os.ModePerm)
	return baseFolder, err
}

// downloadImage downloads an image from the given URL and saves it to the specified folder.
func downloadImage(imgUrl, folderPath string) {
	// Skip SVG images
	if strings.HasSuffix(imgUrl, ".svg") {
		fmt.Printf("Skipped SVG image: %s\n", imgUrl)
		return
	}

	resp, err := http.Get(imgUrl)
	if err != nil {
		fmt.Printf("Failed to download image %s: %v\n", imgUrl, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to download image %s: %v\n", imgUrl, resp.Status)
		return
	}

	imgName := filepath.Base(imgUrl)
	imgName = regexp.MustCompile(`[^\w\.]`).ReplaceAllString(imgName, "_")
	imgPath := filepath.Join(folderPath, imgName)

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read image data %s: %v\n", imgUrl, err)
		return
	}

	if err := ioutil.WriteFile(imgPath, data, 0644); err != nil {
		fmt.Printf("Failed to save image %s: %v\n", imgName, err)
	} else {
		fmt.Printf("Downloaded image: %s\n", imgName)
	}
}

// saveText saves the text content to a file.
func saveText(content, filename string) error {
	return ioutil.WriteFile(filename, []byte(content), 0644)
}

// normalizeURL normalizes the URL to ensure it's absolute.
func normalizeURL(base, rel string) (string, error) {
	if strings.HasPrefix(rel, "//") {
		return "https:" + rel, nil
	}
	u, err := url.Parse(rel)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return rel, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(u).String(), nil
}

// cleanText removes excessive whitespace and newlines from the input text.
func cleanText(input string) string {
	cleaned := strings.TrimSpace(input)
	cleaned = regexp.MustCompile(`\n+`).ReplaceAllString(cleaned, "\n")
	cleaned = regexp.MustCompile(` +`).ReplaceAllString(cleaned, " ")
	return cleaned
}

// scrapePage scrapes a single page and collects URLs and text.
func scrapePage(urlStr string, depth int, baseFolder string) ([]string, error) {
	mu.Lock()
	if visitedUrls[urlStr] || depth >= maxRecursion {
		mu.Unlock()
		return nil, nil
	}
	visitedUrls[urlStr] = true
	mu.Unlock()

	fmt.Printf("Scraping: %s (depth %d)\n", urlStr, depth)

	textFolder := filepath.Join(baseFolder, "text")
	imgFolder := filepath.Join(baseFolder, "img")
	os.MkdirAll(textFolder, os.ModePerm)
	os.MkdirAll(imgFolder, os.ModePerm)

	resp, err := http.Get(urlStr)
	if err != nil {
		fmt.Printf("Failed to retrieve %s: %v\n", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to retrieve %s: %v\n", urlStr, resp.Status)
		return nil, fmt.Errorf("failed to retrieve %s: %v", urlStr, resp.Status)
	}

	// Parse the HTML
	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Printf("Failed to parse HTML from %s: %v\n", urlStr, err)
		return nil, err
	}

	var pageText string
	var imgUrls []string
	var foundUrls []string

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key == "src" {
					imgUrl, err := normalizeURL(urlStr, a.Val)
					if err == nil {
						imgUrls = append(imgUrls, imgUrl)
					}
				}
			}
		} else if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4" || n.Data == "h5" || n.Data == "h6" || n.Data == "li" || n.Data == "a" || n.Data == "span") {
			if n.FirstChild != nil {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						pageText += c.Data + " "
					}
				}
				pageText += "\n"
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	pageText = cleanText(pageText) // Clean the text before saving
	pageFilename := filepath.Join(textFolder, fmt.Sprintf("page_%d.txt", depth))
	if err := saveText(pageText, pageFilename); err != nil {
		fmt.Printf("Failed to save text from %s: %v\n", urlStr, err)
	}

	// Download images in parallel
	var imgWg sync.WaitGroup
	for _, imgUrl := range imgUrls {
		imgWg.Add(1)
		go func(url string) {
			defer imgWg.Done()
			downloadImage(url, imgFolder)
		}(imgUrl)
	}
	imgWg.Wait()

	// Find all new URLs to crawl
	var urlSet = make(map[string]bool)

	var findLinks func(*html.Node)
	findLinks = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					link, err := normalizeURL(urlStr, a.Val)
					if err == nil && !urlSet[link] {
						urlSet[link] = true
						foundUrls = append(foundUrls, link)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}
	findLinks(doc)

	return foundUrls, nil
}

// scrape recursively scrapes URLs for text and images, saving content in unique folders.
func scrape(urlStr string, depth int, baseFolder string, wg *sync.WaitGroup) {
	defer wg.Done()

	semaphore <- struct{}{}        // Acquire a token
	defer func() { <-semaphore }() // Release the token

	foundUrls, err := scrapePage(urlStr, depth, baseFolder)
	if err != nil {
		return
	}

	// Recursively scrape the found URLs
	for _, link := range foundUrls {
		wg.Add(1)
		go scrape(link, depth+1, baseFolder, wg)
	}
}

func main() {
	var wg sync.WaitGroup
	var url string
	fmt.Print("Give a URL to Recursive Scrape: ")
	fmt.Scanln(&url)

	baseFolder, err := getDomainFolder(url)
	if err != nil {
		fmt.Printf("Error creating base folder: %v\n", err)
		return
	}

	wg.Add(1)
	go scrape(url, 0, baseFolder, &wg)
	wg.Wait()
}
