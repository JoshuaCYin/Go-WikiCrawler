# ScholarGraph 🎓🕸️

**ScholarGraph** is a high-performance web crawler and data visualizer built in Go. It recursively scrapes Wikipedia category pages (specifically universities and colleges) to build an interactive relationship graph and statistical demographic charts.

Designed with concurrency and performance in mind, it demonstrates advanced Go patterns like multi-threaded scraping and thread-safe data aggregation.

---

## 🚀 Key Features

- **Concurrent Crawling**: Uses the `Colly` framework with a configurable worker pool to process hundreds of pages in seconds.
- **Recursive Depth**: Configurable crawl depth to explore relationships beyond the starting category.
- **Interactive Visualizations**: Generates rich, interactive HTML reports using `go-echarts`:
  - **Relationship Graph**: Visualizes the connections between universities and linked Wikipedia articles.
  - **Commonality Bar Chart**: Analyzes infobox data fields to find common patterns across institutions.
- **CLI-First Design**: Custom flags allow you to target any Wikipedia category and adjust performance on the fly.
- **Local Web Server**: Automatically serves your generated reports for instant preview.

---

## 🛠️ Tech Stack

- **Language**: [Go](https://go.dev/) (v1.24+)
- **Scraping Framework**: [Colly](http://go-colly.org/)
- **Visualizations**: [Go-ECharts](https://github.com/go-echarts/go-echarts)
- **Concurrency**: Goroutines, `sync.Mutex`, and asynchronous request handling.

---

## 🚦 Getting Started

### Prerequisites
- Go 1.24 or higher installed on your system.

### Installation
1. Clone the repository:
   ```bash
   git clone https://github.com/yourusername/ScholarGraph.git
   cd ScholarGraph
   ```
2. Install dependencies:
   ```bash
   go mod tidy
   ```

### Running the Scraper
Run the program with default settings (scrapes Indiana private universities):
```bash
go run main.go
```

To scrape a **custom category** (e.g., California universities) with **10 concurrent workers**:
```bash
go run main.go -url "https://en.wikipedia.org/wiki/Category:Private_universities_and_colleges_in_California" -workers 10 -depth 2
```

### Viewing Results
Once the scraper finishes, a local server will start at:
👉 **[http://localhost:8089](http://localhost:8089)**

Alternatively, open the files directly:
- `visuals/html/graph.html`
- `visuals/html/barchart.html`

---

## ⚙️ CLI Configuration

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-url` | Indiana Category | Starting Wikipedia Category URL |
| `-workers` | 5 | Number of concurrent page processing routines |
| `-depth` | 2 | Maximum crawl depth (careful: levels >2 grow exponentially) |
| `-graphfile` | `visuals/html/graph.html` | Output path for the relationship graph |
| `-barfile` | `visuals/html/barchart.html` | Output path for the data frequency chart |

---

## 👥 Authors
- **Joshua Yin**
- **Benjamin Kuehner**

---

## 📜 License
This project was developed for **CIS-336**. Use for educational purposes. 

*Note: This project does not copy code from AI or external sources directly. AI was consulted for learning and troubleshooting.*
