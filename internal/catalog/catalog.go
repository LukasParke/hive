package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Template struct {
	Kind           string            `yaml:"kind" json:"kind"`
	Name           string            `yaml:"name" json:"name"`
	Description    string            `yaml:"description" json:"description"`
	Category       string            `yaml:"category" json:"category"`
	Icon           string            `yaml:"icon" json:"icon"`
	Image          string            `yaml:"image" json:"image"`
	Version        string            `yaml:"version" json:"version"`
	Tags           []string          `yaml:"tags" json:"tags"`
	Links          map[string]string `yaml:"links" json:"links"`
	Ports          []string          `yaml:"ports" json:"ports"`
	Volumes        []string          `yaml:"volumes" json:"volumes"`
	Env            map[string]string `yaml:"env" json:"env"`
	Domain         string            `yaml:"domain" json:"domain"`
	Replicas       int               `yaml:"replicas" json:"replicas"`
	DependsOn      []Dependency      `yaml:"depends_on" json:"depends_on"`
	HomepageLabels map[string]string `yaml:"homepage_labels" json:"homepage_labels"`
	TraefikLabels  map[string]string `yaml:"traefik_labels" json:"traefik_labels"`
	NASVolumes     []NASVolume       `yaml:"nas_volumes" json:"nas_volumes"`
	IsStack        bool              `yaml:"stack" json:"is_stack"`
	Services       []StackService    `yaml:"services" json:"services"`
}

type Dependency struct {
	Type    string `yaml:"type" json:"type"`
	Version string `yaml:"version" json:"version"`
}

type NASVolume struct {
	Name          string `yaml:"name" json:"name"`
	SuggestedPath string `yaml:"suggested_path" json:"suggested_path"`
	Description   string `yaml:"description" json:"description"`
}

type StackService struct {
	Name        string            `yaml:"name" json:"name"`
	Image       string            `yaml:"image" json:"image"`
	Ports       []string          `yaml:"ports" json:"ports"`
	Env         map[string]string `yaml:"env" json:"env"`
	Volumes     []string          `yaml:"volumes" json:"volumes"`
	Command     interface{}       `yaml:"command" json:"command,omitempty"`
	Entrypoint  interface{}       `yaml:"entrypoint" json:"entrypoint,omitempty"`
	Healthcheck *Healthcheck      `yaml:"healthcheck" json:"healthcheck,omitempty"`
	User        string            `yaml:"user" json:"user,omitempty"`
	CapAdd      []string          `yaml:"cap_add" json:"cap_add,omitempty"`
	CapDrop     []string          `yaml:"cap_drop" json:"cap_drop,omitempty"`
}

type Healthcheck struct {
	Test        interface{} `yaml:"test" json:"test"`
	Interval    string      `yaml:"interval" json:"interval,omitempty"`
	Timeout     string      `yaml:"timeout" json:"timeout,omitempty"`
	Retries     int         `yaml:"retries" json:"retries,omitempty"`
	StartPeriod string      `yaml:"start_period" json:"start_period,omitempty"`
}

type Catalog struct {
	templates []Template
}

func LoadFromDir(dir string) (*Catalog, error) {
	c := &Catalog{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("read templates dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", entry.Name(), err)
		}

		var tmpl Template
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			return nil, fmt.Errorf("parse template %s: %w", entry.Name(), err)
		}

		if tmpl.Replicas == 0 {
			tmpl.Replicas = 1
		}
		if tmpl.Name == "" {
			tmpl.Name = strings.TrimSuffix(entry.Name(), ext)
		}
		if err := ValidateTemplate(tmpl); err != nil {
			return nil, fmt.Errorf("validate template %s: %w", entry.Name(), err)
		}
		c.templates = append(c.templates, tmpl)
	}

	return c, nil
}

func ValidateTemplate(t Template) error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if t.IsStack {
		if len(t.Services) == 0 {
			return fmt.Errorf("stack template %q must define at least one service", t.Name)
		}
		for _, svc := range t.Services {
			if strings.TrimSpace(svc.Name) == "" {
				return fmt.Errorf("stack template %q has service with empty name", t.Name)
			}
			if strings.TrimSpace(svc.Image) == "" {
				return fmt.Errorf("stack template %q service %q missing image", t.Name, svc.Name)
			}
			for _, p := range svc.Ports {
				if err := validatePortString(p); err != nil {
					return fmt.Errorf("stack template %q service %q invalid port %q: %w", t.Name, svc.Name, p, err)
				}
			}
		}
		return nil
	}

	if strings.TrimSpace(t.Image) == "" {
		return fmt.Errorf("image is required for non-stack template %q", t.Name)
	}
	for _, p := range t.Ports {
		if err := validatePortString(p); err != nil {
			return fmt.Errorf("template %q invalid port %q: %w", t.Name, p, err)
		}
	}
	return nil
}

func validatePortString(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty")
	}
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 1:
		_, err := strconv.Atoi(parts[0])
		return err
	case 2:
		_, err1 := strconv.Atoi(parts[0])
		_, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("must be numeric host:container")
		}
		return nil
	case 3:
		_, err1 := strconv.Atoi(parts[1])
		_, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("must be hostip:hostport:containerport")
		}
		return nil
	default:
		return fmt.Errorf("unsupported port format")
	}
}

func (c *Catalog) List() []Template {
	return c.templates
}

func (c *Catalog) Get(name string) (*Template, error) {
	for _, t := range c.templates {
		if t.Name == name {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template %q not found", name)
}
