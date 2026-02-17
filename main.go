package main

/**
To use this, make a new markdown file. For convention name the file the same as the slug. The md file should be in /markdown

The .md file must then have the following attributes, including the 3 lines ---
those lines separate the tags from the content:

Title: Page title, and title in left sidebar
Slug: slug-of-url
Parent: The name you wish the parent series to be called
Order: number in terms of parent order
Description: Small strap-line description which appears under the title
MetaPropertyTitle: Title for social sharing
MetaDescription: Description ~ 150 - 200 words of the page for SEO.
MetaPropertyDescription: SHORT description for social media sharing.
MetaOgURL: https://www.fluxsec.red/slug-of-url
---
Content goes here
*/

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

type BlogPost struct {
	Title                   string
	Slug                    string
	Parent                  string
	Content                 template.HTML
	Description             string
	Order                   int
	Headers                 []string // these are the in page h2 tags
	MetaDescription         string
	MetaPropertyTitle       string
	MetaPropertyDescription string
	MetaOgURL               string
}

type SidebarData struct {
	Categories []Category
}

type Category struct {
	Name  string
	Pages []BlogPost
	Order int
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()

	// sidebar data
	sidebarData, err := loadSidebarData("./markdown")
	if err != nil {
		log.Fatal(err)
	}

	// template funcs
	r.SetFuncMap(template.FuncMap{
		"loadSidebar": func() SidebarData { return sidebarData },
		"dict":        dict,
	})

	// templates + static
	r.LoadHTMLGlob("templates/*")
	r.Static("/static", "./static")

	// load markdown posts (excluding index.md and about.md from the "posts" list)
	posts, err := loadMarkdownPosts("./markdown")
	if err != nil {
		log.Fatal(err)
	}

	// --- ✅ Load about.md once, compute its slug, and serve it at /<aboutSlug> ---
	aboutSlug := "about" // fallback default
	aboutPath := "./markdown/about.md"

	var aboutPost BlogPost
	var aboutOK bool

	if aboutContent, err := os.ReadFile(aboutPath); err == nil {
		if p, err := parseMarkdownFile(aboutContent); err == nil {
			aboutPost = p
			aboutOK = true

			if s := strings.TrimSpace(p.Slug); s != "" {
				aboutSlug = s
			}
		} else {
			log.Printf("Failed to parse about.md (will fallback to index.md): %v\n", err)
		}
	} else {
		log.Printf("about.md not found (will fallback to index.md): %v\n", err)
	}

	// ✅ ROOT: redirect to /<aboutSlug> (changes browser URL)
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/"+aboutSlug) // 301
	})

	// ✅ About route: actually serves about.md at /<aboutSlug>
	r.GET("/"+aboutSlug, func(c *gin.Context) {
		var post BlogPost
		var err error

		if aboutOK {
			post = aboutPost
		} else {
			// fallback: index.md
			content, readErr := os.ReadFile("./markdown/index.md")
			if readErr != nil {
				log.Printf("Error reading index.md fallback: %v\n", readErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
				return
			}
			post, err = parseMarkdownFile(content)
			if err != nil {
				log.Printf("Error parsing index.md fallback: %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
				return
			}
		}

		sidebarLinks := createSidebarLinks(post.Headers)

		// Usamos layout.html para consistencia con el resto de páginas
		c.HTML(http.StatusOK, "layout.html", gin.H{
			"Title":                   post.Title,
			"Content":                 post.Content,
			"SidebarData":             sidebarData,
			"Headers":                 post.Headers,
			"Description":             post.Description,
			"SidebarLinks":            sidebarLinks,
			"CurrentSlug":             post.Slug,
			"MetaDescription":         post.MetaDescription,
			"MetaPropertyTitle":       post.MetaPropertyTitle,
			"MetaPropertyDescription": post.MetaPropertyDescription,
			"MetaOgURL":               post.MetaOgURL,
		})
	})

	// routes for each blog post (Slug -> /slug)
	for _, post := range posts {
		localPost := post
		if strings.TrimSpace(localPost.Slug) == "" {
			log.Printf("Warning: Post titled '%s' has an empty slug and will not be accessible via a unique URL.\n", localPost.Title)
			continue
		}

		r.GET("/"+localPost.Slug, func(c *gin.Context) {
			sidebarLinks := createSidebarLinks(localPost.Headers)
			c.HTML(http.StatusOK, "layout.html", gin.H{
				"Title":                   localPost.Title,
				"Content":                 localPost.Content,
				"SidebarData":             sidebarData,
				"Headers":                 localPost.Headers,
				"Description":             localPost.Description,
				"SidebarLinks":            sidebarLinks,
				"CurrentSlug":             localPost.Slug,
				"MetaDescription":         localPost.MetaDescription,
				"MetaPropertyTitle":       localPost.MetaPropertyTitle,
				"MetaPropertyDescription": localPost.MetaPropertyDescription,
				"MetaOgURL":               localPost.MetaOgURL,
			})
		})
	}

	r.NoRoute(func(c *gin.Context) {
		c.HTML(http.StatusNotFound, "404.html", gin.H{
			"Title": "Page Not Found",
		})
	})

	// ✅ Render-compatible port binding (uses $PORT when deployed)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r.Run(":" + port)
}

func loadMarkdownPosts(dir string) ([]BlogPost, error) {
	var posts []BlogPost

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		name := file.Name()

		// ✅ Don't treat index/about as "posts" in the routes list
		if name == "index.md" || name == "about.md" {
			continue
		}

		if !strings.HasSuffix(name, ".md") {
			continue
		}

		path := dir + "/" + name
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		post, err := parseMarkdownFile(content)
		if err != nil {
			return nil, err
		}

		posts = append(posts, post)
	}

	return posts, nil
}

func parseMarkdownFile(content []byte) (BlogPost, error) {
	sections := strings.SplitN(string(content), "---", 2)
	if len(sections) < 2 {
		return BlogPost{}, errors.New("invalid markdown format")
	}

	metadata := strings.ReplaceAll(sections[0], "\r", "")
	mdContent := strings.ReplaceAll(sections[1], "\r", "")

	title, slug, parent, description, order,
		metaDescriptionStr, metaPropertyTitleStr,
		metaPropertyDescriptionStr, metaOgURLStr := parseMetadata(metadata)

	htmlContent := mdToHTML([]byte(mdContent))
	headers := extractHeaders([]byte(mdContent))

	return BlogPost{
		Title:                   title,
		Slug:                    slug,
		Parent:                  parent,
		Description:             description,
		Content:                 template.HTML(htmlContent),
		Headers:                 headers,
		Order:                   order,
		MetaDescription:         metaDescriptionStr,
		MetaPropertyTitle:       metaPropertyTitleStr,
		MetaPropertyDescription: metaPropertyDescriptionStr,
		MetaOgURL:               metaOgURLStr,
	}, nil
}

func extractHeaders(content []byte) []string {
	var headers []string
	re := regexp.MustCompile(`(?m)^##\s+(.*)`)
	matches := re.FindAllSubmatch(content, -1)

	for _, match := range matches {
		headers = append(headers, string(match[1]))
	}
	return headers
}

func mdToHTML(md []byte) []byte {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)

	opts := html.RendererOptions{
		Flags: html.CommonFlags | html.HrefTargetBlank,
	}
	renderer := html.NewRenderer(opts)

	doc := p.Parse(md)
	return markdown.Render(doc, renderer)
}

func parseMetadata(metadata string) (
	title string,
	slug string,
	parent string,
	description string,
	order int,
	metaDescription string,
	metaPropertyTitle string,
	metaPropertyDescription string,
	metaOgURL string,
) {
	re := regexp.MustCompile(`(?m)^(\w+):\s*(.+)`)
	matches := re.FindAllStringSubmatch(metadata, -1)

	metaDataMap := make(map[string]string)
	for _, match := range matches {
		if len(match) == 3 {
			metaDataMap[match[1]] = match[2]
		}
	}

	title = metaDataMap["Title"]
	slug = metaDataMap["Slug"]
	parent = metaDataMap["Parent"]
	description = metaDataMap["Description"]

	orderStr := strings.TrimSpace(metaDataMap["Order"])
	metaDescriptionStr := metaDataMap["MetaDescription"]
	metaPropertyTitleStr := metaDataMap["MetaPropertyTitle"]
	metaPropertyDescriptionStr := metaDataMap["MetaPropertyDescription"]
	metaOgURLStr := metaDataMap["MetaOgURL"]

	ord, err := strconv.Atoi(orderStr)
	if err != nil {
		log.Printf("Error converting order from string: %v", err)
		ord = 9999
	}

	return title, slug, parent, description, ord,
		metaDescriptionStr, metaPropertyTitleStr,
		metaPropertyDescriptionStr, metaOgURLStr
}

func loadSidebarData(dir string) (SidebarData, error) {
	var sidebar SidebarData
	categoriesMap := make(map[string]*Category)

	posts, err := loadMarkdownPosts(dir)
	if err != nil {
		return sidebar, err
	}

	// Also include about.md in sidebar categories (since loadMarkdownPosts excludes it)
	// so that "Intro -> About me" still appears.
	aboutPath := dir + "/about.md"
	if aboutContent, err := os.ReadFile(aboutPath); err == nil {
		if aboutPost, err := parseMarkdownFile(aboutContent); err == nil {
			posts = append(posts, aboutPost)
		}
	}

	for _, post := range posts {
		if strings.TrimSpace(post.Parent) == "" {
			continue
		}

		// ✅ FIX: category order = MIN(Order) among its pages
		if cat, exists := categoriesMap[post.Parent]; !exists {
			categoriesMap[post.Parent] = &Category{
				Name:  post.Parent,
				Pages: []BlogPost{post},
				Order: post.Order,
			}
		} else {
			cat.Pages = append(cat.Pages, post)
			if post.Order < cat.Order {
				cat.Order = post.Order
			}
		}
	}

	// map -> slice
	for _, cat := range categoriesMap {
		// sort pages inside each category by Order
		sort.Slice(cat.Pages, func(i, j int) bool {
			return cat.Pages[i].Order < cat.Pages[j].Order
		})
		sidebar.Categories = append(sidebar.Categories, *cat)
	}

	// sort categories by Order (+ tie-breaker by Name)
	sort.Slice(sidebar.Categories, func(i, j int) bool {
		if sidebar.Categories[i].Order == sidebar.Categories[j].Order {
			return strings.ToLower(sidebar.Categories[i].Name) < strings.ToLower(sidebar.Categories[j].Name)
		}
		return sidebar.Categories[i].Order < sidebar.Categories[j].Order
	})

	return sidebar, nil
}

func createSidebarLinks(headers []string) template.HTML {
	var linksHTML string
	for _, header := range headers {
		sanitized := sanitizeHeaderForID(header)
		linksHTML += fmt.Sprintf(`<li><a href="#%s">%s</a></li>`, sanitized, header)
	}
	return template.HTML(linksHTML)
}

func sanitizeHeaderForID(header string) string {
	header = strings.ToLower(header)
	header = strings.ReplaceAll(header, " ", "-")
	header = regexp.MustCompile(`[^a-z0-9\-]`).ReplaceAllString(header, "")
	return header
}

func dict(values ...interface{}) (map[string]interface{}, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("invalid dict call")
	}
	out := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil, errors.New("dict keys must be strings")
		}
		out[key] = values[i+1]
	}
	return out, nil
}
