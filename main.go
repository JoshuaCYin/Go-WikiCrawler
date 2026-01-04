/*
====================================================================================================================
CIS-336 Project
Benjamin Kuehner, Joshua Yin
Web scraper that collects information from Wikipedia about private universities and colleges and articles linked to them
We did not copy code from AI or directly from any other source. AI was consulted for learning, debugging, and troubleshooting.

The basic command to run this program is: go run main.go
====================================================================================================================
*/

package main

import (
	"flag" // for command-line flag parsing
	"fmt"  // for formatted I/O
	"log"  // for logging server requests
	"log/slog"
	"net/http" // for serving the graph and bar chart html
	"os"
	"sort" // for sorting data for the bar chart
	"strings"
	"sync" // for concurrency control
	"time" // for timing how long the program takes to run

	"github.com/gocolly/colly" // for web scraping

	// We are using the go-echarts library to create the graph and chart.
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
)

// ======= Data Structures =======

// University struct to hold title, url, and any data that was scraped from the wikipedia infobox
type University struct {
	Title  string
	URL    string
	Fields map[string]string // map to hold field name -> field value pairs (e.g., "Website" -> "www.example.edu")
}

// PageGraph struct to hold the data for our relationship graph.
type PageGraph struct {
	Nodes           map[string]struct{} // set of all node names (including non-university)
	UniversityNodes map[string]struct{} // set of university node names
	Links           []opts.GraphLink    // list of connections (e.g., "STARTING_CATEGORY" -> "Purdue_University")
	mutex           sync.Mutex          // mutex lock to protect this data during concurrent/parallel access by the workers
}

// This constant is at the package level (outside functions) so that both main() and generateGraphChart() can see it.
const CategoryRoot = "STARTING CATEGORY"

// ======= Main Function =======

func main() {
	// ======= Custom command-line flags =======
	// Example command to run: go run main.go -url "https://en.wikipedia.org/wiki/Category:Private_universities_and_colleges_in_California" -workers 5 -depth 2 -graphfile "visuals/html/graph_CA.html" -barfile "visuals/html/bar_CA.html"

	urlFlag := flag.String("url",
		"https://en.wikipedia.org/wiki/Category:Private_universities_and_colleges_in_Indiana",
		"Category URL") // sets the starting category URL for scraping
	workers := flag.Int("workers", 5, "number of concurrent workers")                                  // sets the number of concurrent page processing routines
	maxDepth := flag.Int("depth", 2, "Maximum crawl depth")                                            // sets how deep the crawler can explore. beyond 2, it may take too long.
	graphFile := flag.String("graphfile", "visuals/html/graph.html", "Output graph HTML file")         // sets the name of the graph HTML file
	barChartFile := flag.String("barfile", "visuals/html/barchart.html", "Output bar chart HTML file") // sets the name of the bar chart HTML file

	flag.Parse()

	// ======= Setup =======

	// Setup for structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)) // create a new logger that outputs to standard output
	slog.SetDefault(logger)

	startTime := time.Now() // record the start time of the program

	// Results storage setup. results is used by the log and output.
	var results []University    // Slice to hold scraped university data
	var resultsMutex sync.Mutex // lock to protect results slice

	// For the Bar Chart: a map to count how many times we see each field (e.g., "Endowment": 15, "Students": 18)
	fieldCounts := make(map[string]int) // 'make' initializes
	var fieldCountsMutex sync.Mutex     // lock to protect this map

	// For the Graph: an instance of a new PageGraph struct
	graphData := &PageGraph{
		Nodes:           make(map[string]struct{}),
		UniversityNodes: make(map[string]struct{}),
		Links:           make([]opts.GraphLink, 0),
	}

	// Add the first/root/central node to the graph prior to scraping
	graphData.Nodes[CategoryRoot] = struct{}{}

	// ======= Colly Collectors (Scrapers) =======

	// Main collector for category pages
	c := colly.NewCollector(
		colly.AllowedDomains("en.wikipedia.org"), // limit scraper to wikipedia
		colly.Async(true),                        // enable asynchronous requests
	)

	// Clone the main collector for article pages
	articleCollector := c.Clone()
	articleCollector.Async = true // enable asynchronous requests

	// Set number of concurrent requests for both collectors using workers flag
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: *workers,
	})

	articleCollector.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: *workers,
	})

	// Disallow revisiting URLs
	c.AllowURLRevisit = false
	articleCollector.AllowURLRevisit = false

	// ======= Category Page Crawling/Scraping =======

	// Find article links on the category page.
	// c.OnHTML registers a callback function that runs whenever the collector ('c') finds an HTML element matching the CSS selector.
	// This one: in the div with id=mw-pages, finds any <ul>, then any <li>, then any <a> tag that has an href attribute
	c.OnHTML("div#mw-pages ul li a[href]", func(e *colly.HTMLElement) {
		// Get the absolute URL of the link
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if strings.Contains(link, "Category:") { // Exit if it's a sub-category link. We only want articles at this point.
			return
		}

		// Get the current depth from the context
		currentDepth := e.Request.Ctx.GetAny("depth").(int)
		if currentDepth > *maxDepth { // Exit if we are out of the max depth
			return
		}

		// Get article title from href
		title := strings.TrimPrefix(e.Attr("href"), "/wiki/")
		slog.Info("Found university in category", "title", title, "depth", currentDepth)

		// Graph category page to university node
		graphData.mutex.Lock()
		graphData.Nodes[title] = struct{}{} // add university node
		graphData.UniversityNodes[title] = struct{}{}
		graphData.Links = append(graphData.Links, opts.GraphLink{ // add link from category to university
			Source: CategoryRoot,
			Target: title,
		})
		graphData.mutex.Unlock()

		// Ctx is how we pass data (like depth) between requests
		newCtx := colly.NewContext()
		newCtx.Put("depth", currentDepth+1)

		// Enqueue the article request with the new context
		articleCollector.Request("GET", link, nil, newCtx, nil)
	})

	// Handle "next page" links on the category page to we ensure we don't leave out anything
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		if !strings.Contains(strings.ToLower(e.Text), "next page") {
			return
		}

		// Check depth
		currentDepth := e.Request.Ctx.GetAny("depth").(int)
		if currentDepth > *maxDepth {
			return
		}

		// Get absolute URL of the next page link
		nextURL := e.Request.AbsoluteURL(e.Attr("href"))

		// Enqueue the next page with incremented depth
		newCtx := colly.NewContext()
		newCtx.Put("depth", currentDepth+1)
		e.Request.Ctx = newCtx
		e.Request.Visit(nextURL)
	})

	// Log category page requests
	c.OnRequest(func(r *colly.Request) {
		slog.Info("Visiting category page", "url", r.URL)
	})

	// Handle category page errors
	c.OnError(func(r *colly.Response, err error) {
		slog.Error("Category page error", "url", r.Request.URL, "status", r.StatusCode, "error", err)
	})

	// ======= Handle Article Pages =======
	// This is where we scrape the individual article pages.
	// Only recurses through infobox links.

	// This callback function runs for articleCollector when it finds a table element with class infobox and vcard.
	articleCollector.OnHTML("table.infobox.vcard", func(e *colly.HTMLElement) {

		// Get a title for the university from the URL path
		title := strings.TrimPrefix(e.Request.URL.Path, "/wiki/")

		// Check depth
		currentDepth := e.Request.Ctx.GetAny("depth").(int)
		if currentDepth > *maxDepth {
			return
		}

		// Initialize University struct
		university := University{
			Title:  title,
			URL:    e.Request.URL.String(),
			Fields: make(map[string]string), // initialize a new map for this university's fields
		}

		// Scrape individual article pages (part 2): scrape infobox fields and links
		e.ForEach("tr", func(_ int, row *colly.HTMLElement) { // loop over all "tr" (table row) elements
			th := row.ChildText("th") // get the text inside the <th> (table header) tag
			td := row.ChildText("td") // get the text inside the <td> (table data) tag

			// Field
			if th != "" && td != "" { // only add to map if both th and td are not empty
				university.Fields[th] = td

				// Update field count for the bar chart
				fieldCountsMutex.Lock()
				fieldCounts[th]++ // increment the counter for this field (e.g., "Website")
				fieldCountsMutex.Unlock()
			}

			// Infobox links
			row.ForEach("td a[href]", func(_ int, linkElem *colly.HTMLElement) {
				href := linkElem.Attr("href") // get the href attribute of the <a> tag

				// Wikipedia article links start with "/wiki/" and do not contain ":" (which indicates special pages)
				if !strings.HasPrefix(href, "/wiki/") {
					return
				}
				if strings.Contains(href, ":") {
					return
				}

				target := strings.TrimPrefix(href, "/wiki/")

				// Add the article as a new node (the map handles duplicates)
				graphData.mutex.Lock()
				graphData.Nodes[target] = struct{}{}
				// Add a link from the current university to this linked article
				// It also creates the edges in the graph between non-university articles
				graphData.Links = append(graphData.Links, opts.GraphLink{
					Source: university.Title, // article we are currently on
					Target: target,           // article we just found a link to
				})
				graphData.mutex.Unlock()

				// Recusively visit
				newCtx := colly.NewContext()
				newCtx.Put("depth", currentDepth+1)
				articleCollector.Request("GET", e.Request.AbsoluteURL(href), nil, newCtx, nil)
			})
		})

		// ======= Save Data =======

		// Append the university to the results slice.
		resultsMutex.Lock()
		results = append(results, university) // add this university to the results slice
		resultsMutex.Unlock()

		// Log scraped article
		slog.Info("Scraped article", "title", title, "fields", len(university.Fields))
	})

	// Log article requests and handle errors
	articleCollector.OnRequest(func(r *colly.Request) {
		slog.Info("Visiting article", "url", r.URL)
	})

	// Handle article page errors
	articleCollector.OnError(func(r *colly.Response, err error) {
		slog.Error("Article error", "url", r.Request.URL, "status", r.StatusCode, "error", err)

		if r.StatusCode == 429 || r.StatusCode >= 500 {
			// 429 = Too Many Requests. 500+ = Server Error.
			slog.Info("Error: retrying", "url", r.Request.URL, "status", r.StatusCode)
			// Retry if we hit these errors.
			r.Request.Retry()
		}
	})

	// ======= Start Scraping Process =======

	slog.Info("Starting scraper", "url", *urlFlag, "workers", *workers)

	// Create initial context
	ctx := colly.NewContext()
	ctx.Put("depth", 1) // starting depth = 1

	// Start by visiting the category URL with the initial context
	var err = c.Request("GET", *urlFlag, nil, ctx, nil) // upon this line, the category collector 'c' starts processing
	if err != nil {
		slog.Error("Failed to visit category URL", "error", err)
		os.Exit(1)
	}

	// At this point, collectors are running asynchronously.

	// Wait for everything to finish
	c.Wait()                // category collector
	articleCollector.Wait() // article collector

	// ======= Generate Visuals =======
	// Generate the charts
	generateGraphChart(graphData, *graphFile)
	generateBarChart(fieldCounts, *barChartFile)

	// Calculate total duration
	duration := time.Since(startTime)

	// Final log message
	slog.Info("Scraping complete",
		"articles", len(results),
		"duration", duration,
		"graph", *graphFile,
		"chart", *barChartFile,
	)

	// Also print to standard output
	fmt.Printf("\nScraped %d articles in %v\n", len(results), duration)
	fmt.Printf("Graph HTML: %s\n", *graphFile)
	fmt.Printf("Bar Chart HTML: %s\n", *barChartFile)

	// Serve the visuals over HTTP
	fs := http.FileServer(http.Dir("visuals/html"))
	log.Println("Running server at http://localhost:8089")
	log.Fatal(http.ListenAndServe("localhost:8089", logRequest(fs)))
}

// logRequest logs incoming HTTP requests.
func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

// ======= Bar Chart =======
// Creates a bar chart showing the most common data fields found.
func generateBarChart(counts map[string]int, filename string) {
	slog.Info("Generating bar chart...", "file", filename)

	const NumTop = 20 // number of top fields to show

	// Go's maps are not sorted, so we must copy keys/values to a slice and then sort that slice.

	type kv struct { // struct to hold key-value pairs
		Key   string
		Value int
	}

	var sorted []kv // slice to hold key-value pairs
	total := 0      // total count of all fields

	// Copy map to slice
	for k, v := range counts {
		sorted = append(sorted, kv{Key: k, Value: v})
		total += v
	}

	// Sort the slice by value in descending order
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	// Keep only the top NumTop entries because we don't want the chart to be too cluttered
	if len(sorted) > NumTop {
		sorted = sorted[:NumTop]
	}

	// Prepare data for the bar chart
	categories := make([]string, 0, len(sorted))
	barValues := make([]opts.BarData, 0, len(sorted))

	// We want the highest counts at the top of the horizontal bar chart, so we iterate in reverse order.
	for i := len(sorted) - 1; i >= 0; i-- { // reverse for horizontal bars
		item := sorted[i] // current key-value pair

		// Calculate percent and check for total being zero to avoid division by zero
		percent := 0.0
		if total > 0 {
			percent = (float64(item.Value) / float64(total)) * 100.0
		}

		// Create bar data with value and label showing count and percentage
		categories = append(categories, item.Key) // category is the field name
		barValues = append(barValues, opts.BarData{
			Value: item.Value,
			Label: &opts.Label{
				Show:      opts.Bool(true),
				Position:  "right",
				Formatter: fmt.Sprintf("%d (%.1f%%)", item.Value, percent),
			},
		})
	}

	// Create the bar chart
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{ // set chart title
			Title: "Top 20 Most Common Infobox Fields",
		}),
		charts.WithTooltipOpts(opts.Tooltip{ // enable tooltips
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{ // enable data zooming
			Type:       "slider",
			YAxisIndex: []int{0},
		}),
		charts.WithXAxisOpts(opts.XAxis{ // x-axis is value type
			Type: "value",
		}),
		charts.WithYAxisOpts(opts.YAxis{ // y-axis is category type
			Type: "category",
			Data: categories,
		}),
	)

	// Add the bar series
	bar.AddSeries("Field Count", barValues)

	// Create a page to show the bar
	page := components.NewPage()
	page.AddCharts(bar)

	// Render to HTML file
	f, err := os.Create(filename)
	if err != nil {
		slog.Error("Failed to create bar chart HTML file", "error", err)
		return
	}
	defer f.Close()

	page.Render(f) // render the page to the file
}

// ======= Graph Chart =======
// Creates a graph showing relationships between pages.
func generateGraphChart(graphData *PageGraph, filename string) {
	slog.Info("Generating graph chart...", "file", filename)

	// Define categories. Each category can have different styling.
	categories := []*opts.GraphCategory{
		{Name: "Category Page"},  // index 0
		{Name: "University"},     // index 1
		{Name: "Linked Article"}, // index 2
	}

	// Count links per node
	linkCount := make(map[string]int)

	// Iterate over all links to count how many times each node appears as source or target
	for _, link := range graphData.Links {
		source, ok := link.Source.(string) // ok tells us if the type assertion succeeded
		if ok {
			linkCount[source]++
		}
		target, ok := link.Target.(string)
		if ok {
			linkCount[target]++
		}
	}

	// Find the maximum link count for normalization
	maxLinkCount := 0
	for _, count := range linkCount {
		if count > maxLinkCount {
			maxLinkCount = count
		}
	}

	// Create nodes with size proportional to link count
	minSize := 10.0
	maxSize := 40.0

	// Create graph nodes
	nodes := make([]opts.GraphNode, 0, len(graphData.Nodes))
	for name := range graphData.Nodes {
		count := linkCount[name]       // number of links
		symbolSize := float32(minSize) // default size

		// Scale size based on link count
		if maxLinkCount > 0 {
			symbolSize = float32(minSize + (maxSize-minSize)*float64(count)/float64(maxLinkCount))
		}

		// Determine category
		categoryIndex := 2 // default linked article
		if name == CategoryRoot {
			categoryIndex = 0
		} else if _, isUniversity := graphData.UniversityNodes[name]; isUniversity {
			categoryIndex = 1
		}

		// Append the node
		nodes = append(nodes, opts.GraphNode{
			Name:       name,
			SymbolSize: symbolSize,
			Category:   categoryIndex,
		})
	}

	// Create graph chart
	graph := charts.NewGraph()

	// Set global options
	graph.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{ // set chart size
			Width:  "100%",
			Height: "100vh", // full viewport height
		}),
		charts.WithTitleOpts(opts.Title{ // set chart title
			Title: "Scraped Page Relationships",
		}),
		charts.WithTooltipOpts(opts.Tooltip{ // enable tooltips
			Show: opts.Bool(true),
		}),
		charts.WithLegendOpts(opts.Legend{ // show legend
			Show: opts.Bool(true),
		}),
	)

	// Add graph series. This is where we add the nodes and links to the graph. A series is like a dataset.
	graph.AddSeries(
		"graph",         // series name
		nodes,           // graph nodes
		graphData.Links, // graph links
		charts.WithGraphChartOpts(opts.GraphChart{ // graph-specific options
			Layout:             "force", // use force-directed layout so nodes are spaced
			Force:              &opts.GraphForce{Repulsion: 1000, EdgeLength: 50},
			Roam:               opts.Bool(true), // enable drag/zoom
			Categories:         categories,
			FocusNodeAdjacency: opts.Bool(true), // highlight connected nodes on hover
			Draggable:          opts.Bool(true), // make nodes draggable
		}),
		charts.WithLabelOpts(opts.Label{ // show labels
			Show: opts.Bool(false), // can use false to hide labels for clarity
		}),
	)

	// Create a page to hold the graph
	page := components.NewPage()
	page.SetLayout(components.PageNoneLayout) // no extra layout so graph uses full space
	page.AddCharts(graph)

	// Render to HTML file
	f, err := os.Create(filename)
	if err != nil {
		slog.Error("Failed to create graph HTML file", "error", err)
		return
	}
	defer f.Close()

	page.Render(f) // render the page to the file
}
