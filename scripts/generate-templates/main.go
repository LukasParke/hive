package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	metaURL      = "https://raw.githubusercontent.com/Dokploy/templates/canary/meta.json"
	composeURL   = "https://raw.githubusercontent.com/Dokploy/templates/canary/blueprints/%s/docker-compose.yml"
	iconBaseURL  = "https://cdn.jsdelivr.net/gh/selfhst/icons/svg/%s.svg"
	outputDir    = "templates"
)

type DokployMeta struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Logo        string            `json:"logo"`
	Links       map[string]string `json:"links"`
	Tags        []string          `json:"tags"`
}

type HiveTemplate struct {
	Kind        string              `yaml:"kind"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Category    string              `yaml:"category"`
	Tags        []string            `yaml:"tags,omitempty"`
	Icon        string              `yaml:"icon"`
	Version     string              `yaml:"version,omitempty"`
	Links       map[string]string   `yaml:"links,omitempty"`
	Image       string              `yaml:"image"`
	Ports       []string            `yaml:"ports,omitempty"`
	Volumes     []string            `yaml:"volumes,omitempty"`
	Env         map[string]string   `yaml:"env,omitempty"`
	IsStack     bool                `yaml:"stack,omitempty"`
	Services    []HiveStackService  `yaml:"services,omitempty"`
}

type HiveStackService struct {
	Name       string            `yaml:"name"`
	Image      string            `yaml:"image"`
	Ports      []string          `yaml:"ports,omitempty"`
	Env        map[string]string `yaml:"env,omitempty"`
	Volumes    []string          `yaml:"volumes,omitempty"`
	Command    interface{}       `yaml:"command,omitempty"`
}

type ComposeFile struct {
	Version  string                        `yaml:"version"`
	Services map[string]ComposeServiceSpec `yaml:"services"`
	Volumes  map[string]interface{}        `yaml:"volumes"`
}

type ComposeServiceSpec struct {
	Image       string      `yaml:"image"`
	Build       interface{} `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	Volumes     []string    `yaml:"volumes"`
	Environment interface{} `yaml:"environment"`
	Command     interface{} `yaml:"command"`
	Restart     string      `yaml:"restart"`
	DependsOn   interface{} `yaml:"depends_on"`
	EnvFile     interface{} `yaml:"env_file"`
}

var tagToCategoryMap = map[string]string{
	"analytics":      "analytics",
	"automation":     "automation",
	"backup":         "backup",
	"blogging":       "publishing",
	"bookmarks":      "productivity",
	"budgeting":      "finance",
	"calendar":       "productivity",
	"chat":           "communication",
	"ci-cd":          "development",
	"cms":            "cms",
	"code":           "development",
	"collaboration":  "productivity",
	"communication":  "communication",
	"crm":            "business",
	"dashboard":      "monitoring",
	"database":       "databases",
	"databases":      "databases",
	"developer-tools": "development",
	"development":    "development",
	"dns":            "networking",
	"documents":      "productivity",
	"documentation":  "documentation",
	"download":       "media",
	"downloader":     "media",
	"email":          "communication",
	"ecommerce":      "business",
	"file-sharing":   "storage",
	"files":          "storage",
	"finance":        "finance",
	"forms":          "productivity",
	"forum":          "communication",
	"git":            "development",
	"home-automation": "home-automation",
	"hosting":        "hosting",
	"identity":       "security",
	"iot":            "home-automation",
	"knowledge-base": "documentation",
	"logs":           "monitoring",
	"mail":           "communication",
	"manga":          "media",
	"media":          "media",
	"messaging":      "communication",
	"money":          "finance",
	"monitoring":     "monitoring",
	"music":          "media",
	"networking":     "networking",
	"notes":          "productivity",
	"notification":   "communication",
	"observability":  "monitoring",
	"office":         "productivity",
	"password":       "security",
	"pentest":        "security",
	"photo":          "media",
	"photos":         "media",
	"privacy":        "security",
	"productivity":   "productivity",
	"project-management": "productivity",
	"proxy":          "networking",
	"publishing":     "publishing",
	"rss":            "media",
	"search":         "search",
	"security":       "security",
	"self-hosted":    "",
	"social":         "communication",
	"status":         "monitoring",
	"storage":        "storage",
	"streaming":      "media",
	"surveillance":   "security",
	"testing":        "development",
	"tracker":        "media",
	"url-shortener":  "utilities",
	"video":          "media",
	"vpn":            "networking",
	"webmail":        "communication",
	"wiki":           "documentation",
	"workflow":       "automation",
}

func mapCategory(tags []string) string {
	for _, tag := range tags {
		tag = strings.ToLower(tag)
		if cat, ok := tagToCategoryMap[tag]; ok && cat != "" {
			return cat
		}
	}
	if len(tags) > 0 {
		return strings.ToLower(tags[0])
	}
	return "other"
}

func iconURL(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "(", "")
	slug = strings.ReplaceAll(slug, ")", "")
	return fmt.Sprintf(iconBaseURL, slug)
}

func cleanLinks(links map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range links {
		if v != "" {
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseEnv(env interface{}) map[string]string {
	result := make(map[string]string)
	if env == nil {
		return result
	}
	switch v := env.(type) {
	case map[string]interface{}:
		for key, val := range v {
			s := resolveEnvVars(fmt.Sprintf("%v", val))
			result[key] = s
		}
	case []interface{}:
		for _, item := range v {
			s := resolveEnvVars(fmt.Sprintf("%v", item))
			parts := strings.SplitN(s, "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = parts[1]
			}
		}
	}
	return result
}

func resolveImage(image string) string {
	return resolveEnvVars(image)
}

func cleanVolumes(vols []string) []string {
	var result []string
	for _, v := range vols {
		if strings.Contains(v, "..") || strings.Contains(v, "./") {
			continue
		}
		result = append(result, resolveEnvVars(v))
	}
	return result
}

// resolveEnvVars resolves ${VAR:-default} patterns to just the default value,
// and strips ${VAR} patterns that have no default.
func resolveEnvVars(s string) string {
	for {
		start := strings.Index(s, "${")
		if start == -1 {
			break
		}
		endIdx := strings.Index(s[start:], "}")
		if endIdx == -1 {
			break
		}
		endIdx += start
		inner := s[start+2 : endIdx]

		var replacement string
		if idx := strings.Index(inner, ":-"); idx != -1 {
			replacement = inner[idx+2:]
		} else if idx := strings.Index(inner, "-"); idx != -1 {
			replacement = inner[idx+1:]
		} else {
			replacement = ""
		}

		s = s[:start] + replacement + s[endIdx+1:]
	}
	return s
}

func extractPorts(ports []string) []string {
	var result []string
	for _, p := range ports {
		parts := strings.Split(p, ":")
		if len(parts) >= 1 {
			port := parts[len(parts)-1]
			port = strings.Split(port, "/")[0]
			result = append(result, port)
		}
	}
	return result
}

func fetchJSON(url string, target interface{}) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func fetchCompose(id string) (*ComposeFile, error) {
	url := fmt.Sprintf(composeURL, id)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, id)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var cf ComposeFile
	if err := yaml.Unmarshal(body, &cf); err != nil {
		return nil, fmt.Errorf("parse compose for %s: %w", id, err)
	}
	return &cf, nil
}

func existingTemplates(dir string) map[string]bool {
	existing := make(map[string]bool)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return existing
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		existing[name] = true
	}
	return existing
}

func main() {
	fmt.Println("Fetching Dokploy meta.json...")
	var metas []DokployMeta
	if err := fetchJSON(metaURL, &metas); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch meta.json: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d templates in Dokploy catalog\n", len(metas))

	existing := existingTemplates(outputDir)
	fmt.Printf("Found %d existing templates in %s/\n", len(existing), outputDir)

	created := 0
	skipped := 0
	failed := 0

	for i, meta := range metas {
		if existing[meta.ID] {
			skipped++
			continue
		}

		fmt.Printf("[%d/%d] Processing %s... ", i+1, len(metas), meta.ID)

		tmpl := HiveTemplate{
			Kind:        "Template",
			Name:        meta.ID,
			Description: meta.Description,
			Category:    mapCategory(meta.Tags),
			Tags:        meta.Tags,
			Icon:        iconURL(meta.Name),
			Version:     meta.Version,
			Links:       cleanLinks(meta.Links),
		}

		cf, err := fetchCompose(meta.ID)
		if err != nil {
			fmt.Printf("compose fetch failed: %v, creating metadata-only template\n", err)
			tmpl.Image = strings.ToLower(meta.ID) + ":" + meta.Version
			writeTemplate(tmpl)
			created++
			continue
		}

		if len(cf.Services) == 0 {
			fmt.Println("no services in compose, skip")
			failed++
			continue
		}

		if len(cf.Services) == 1 {
			for name, svc := range cf.Services {
				if svc.Image == "" {
					fmt.Printf("no image for %s, skip\n", name)
					failed++
					continue
				}
				tmpl.Image = resolveImage(svc.Image)
				tmpl.Ports = extractPorts(svc.Ports)
				tmpl.Volumes = cleanVolumes(svc.Volumes)
				env := parseEnv(svc.Environment)
				if len(env) > 0 {
					tmpl.Env = env
				}
			}
		} else {
			tmpl.IsStack = true
			var services []HiveStackService

			// Sort service names for deterministic output
			var svcNames []string
			for name := range cf.Services {
				svcNames = append(svcNames, name)
			}
			sort.Strings(svcNames)

			for _, name := range svcNames {
				svc := cf.Services[name]
				if svc.Image == "" {
					continue
				}
				hs := HiveStackService{
					Name:    name,
					Image:   resolveImage(svc.Image),
					Ports:   extractPorts(svc.Ports),
					Volumes: cleanVolumes(svc.Volumes),
					Command: svc.Command,
				}
				env := parseEnv(svc.Environment)
				if len(env) > 0 {
					hs.Env = env
				}
				services = append(services, hs)

				// Primary service: first one with ports
				if tmpl.Image == "" && len(svc.Ports) > 0 {
					tmpl.Image = resolveImage(svc.Image)
					tmpl.Ports = extractPorts(svc.Ports)
				}
			}

			// If no service had ports, use first service image
			if tmpl.Image == "" && len(services) > 0 {
				tmpl.Image = services[0].Image
			}

			tmpl.Services = services
		}

		if tmpl.Image == "" {
			fmt.Println("no image found, skip")
			failed++
			continue
		}

		writeTemplate(tmpl)
		created++
		fmt.Println("OK")

		// Small delay to be nice to GitHub
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("\nDone! Created: %d, Skipped (existing): %d, Failed: %d\n", created, skipped, failed)
}

func writeTemplate(tmpl HiveTemplate) {
	filename := filepath.Join(outputDir, tmpl.Name+".yaml")
	data, err := yaml.Marshal(tmpl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal %s: %v\n", tmpl.Name, err)
		return
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", filename, err)
	}
}
