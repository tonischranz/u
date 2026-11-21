package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	unicodeURL = "https://unicode.org/Public/UCD/latest/ucd/UnicodeData.txt"
	localFile  = "UnicodeData.txt"
	listenAddr = ":8080"
)

type UnicodeChar struct {
	CodePoint string `json:"codePoint"`
	Char      string `json:"char"`
	Name      string `json:"name"`
	Category  string `json:"category"`
}

var characters []UnicodeChar

func main() {
	if err := ensureFileExists(localFile, unicodeURL); err != nil {
		log.Fatalf("Failed to obtain UnicodeData.txt: %v", err)
	}

	var err error
	characters, err = parseUnicodeData(localFile)
	if err != nil {
		log.Fatalf("Failed to parse UnicodeData.txt: %v", err)
	}
	log.Printf("Loaded %d Unicode characters into memory.", len(characters))

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/search", handleSearch)

	log.Printf("Server listening on http://localhost%s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func ensureFileExists(filepath, url string) error {
	if _, err := os.Stat(filepath); err == nil {
		log.Printf("Found local cache: %s", filepath)
		return nil
	}

	log.Printf("Downloading %s...", url)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad HTTP status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	log.Printf("Saved %s locally.", filepath)
	return err
}

func parseUnicodeData(filepath string) ([]UnicodeChar, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var list []UnicodeChar
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ";")
		if len(parts) < 11 {
			continue
		}

		hexCode := parts[0]
		name := parts[1]
		category := parts[2]
		oldName := parts[10]

		// Use Unicode 1.0 name for ASCII control characters
		if name == "<control>" && oldName != "" {
			name = oldName + " (control)"
		}

		// Skip surrogate ranges or private use markers that cannot be rendered
		if strings.HasSuffix(name, ", First>") || strings.HasSuffix(name, ", Last>") {
			continue
		}

		codeVal, err := strconv.ParseInt(hexCode, 16, 32)
		if err != nil {
			continue
		}

		charStr := ""
		// Avoid rendering raw control/surrogate characters in HTML
		if category != "Cc" && category != "Cs" {
			charStr = string(rune(codeVal))
		}

		list = append(list, UnicodeChar{
			CodePoint: "U+" + hexCode,
			Char:      charStr,
			Name:      name,
			Category:  category,
		})
	}

	return list, scanner.Err()
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(strings.ToUpper(r.URL.Query().Get("q")))
	var results []UnicodeChar

	if query != "" {
		terms := strings.Fields(query)
		for _, c := range characters {
			match := true
			for _, term := range terms {
				if !strings.Contains(c.Name, term) && !strings.Contains(c.CodePoint, term) {
					match = false
					break
				}
			}
			if match {
				results = append(results, c)
				if len(results) >= 100 { // Cap results for fast rendering
					break
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("index").Parse(htmlTemplate))
	tmpl.Execute(w, nil)
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>&#x2692; - U+ --</title>
	<style>
		@import url('https://fonts.googleapis.com/css2?family=Noto+Sans');
	 	@import url('https://fonts.googleapis.com/css2?family=Noto+Sans+Mono');
        @import url('https://fonts.googleapis.com/css2?family=Noto+Color+Emoji&display=swap');

		body { font-family: "Noto Sans", "Noto Color Emoji"; max-width: 72rem; margin: 2rem auto; padding: 0 1rem; background-color: #111; color: #eee; }
		input { width: 100%; padding: 12px; font-size: 16px; border: 2px solid #ccc; border-radius: 6px; box-sizing: border-box; background-color: #7772; color: #ddd; }
		input:focus { outline: none; border-color: #0066cc; }
		table { width: 100%; border-collapse: collapse; margin-top: 1.5rem; }
		th, td { text-align: left; padding: 10px; border-bottom: 1px solid #eee; }
		.glyph { font-size: 1.6rem; text-align: center; width: 60px; }
		.code { font-family: monospace; color: #777; width: 100px; }
		.cat { font-family: monospace; color: #888; width: 60px; }

		@media (prefers-color-scheme: light) {
			body { background-color: #eee; color: #111; }
			input { border-color: #ccc; color: #333; }
		}
	</style>
</head>
<body>
	<h1>U+ - &#x1F9D4;&#x1cdf; --&#x1cdf;&#x1F9D4;</h1>
	<input type="text" id="search" placeholder="Search by name or code point (e.g., SNOWMAN, ARROW, U+1F600)..." autofocus>
	<table>
		<thead>
			<tr>
				<th>Char</th>
				<th>Code</th>
				<th>Name</th>
				<th>Cat</th>
			</tr>
		</thead>
		<tbody id="results"></tbody>
	</table>

	<script>
		const input = document.getElementById('search');
		const tbody = document.getElementById('results');
		let timeout = null;

		input.addEventListener('input', () => {
			clearTimeout(timeout);
			timeout = setTimeout(async () => {
				const q = input.value.trim();
				if (!q) { tbody.innerHTML = ''; return; }
				const res = await fetch('/search?q=' + encodeURIComponent(q));
				const data = await res.json();
				tbody.innerHTML = (data || []).map(item => 
					'<tr>' +
						'<td class="glyph">' + (item.char || '—') + '</td>' +
						'<td class="code">' + item.codePoint + '</td>' +
						'<td>' + item.name + '</td>' +
						'<td class="cat">' + item.category + '</td>' +
					'</tr>'
				).join('');
			}, 150);
		});
	</script>
</body>
</html>`
