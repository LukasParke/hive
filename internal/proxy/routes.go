package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lholliger/hive/internal/store"
	"gopkg.in/yaml.v3"
)

type DynamicConfig struct {
	HTTP HTTPConfig `yaml:"http"`
}

type HTTPConfig struct {
	Routers     map[string]RouterConfig     `yaml:"routers"`
	Services    map[string]ServiceConfig     `yaml:"services"`
	Middlewares map[string]MiddlewareConfig  `yaml:"middlewares,omitempty"`
}

type RouterConfig struct {
	Rule        string            `yaml:"rule"`
	Service     string            `yaml:"service"`
	EntryPoints []string          `yaml:"entryPoints"`
	TLS         *TLSRouterConfig  `yaml:"tls,omitempty"`
	Middlewares []string          `yaml:"middlewares,omitempty"`
}

type TLSRouterConfig struct {
	CertResolver string   `yaml:"certResolver,omitempty"`
	Domains      []Domain `yaml:"domains,omitempty"`
}

type Domain struct {
	Main string   `yaml:"main"`
	SANs []string `yaml:"sans,omitempty"`
}

type ServiceConfig struct {
	LoadBalancer LBConfig `yaml:"loadBalancer"`
}

type LBConfig struct {
	Servers []ServerEntry `yaml:"servers"`
}

type ServerEntry struct {
	URL string `yaml:"url"`
}

type MiddlewareConfig map[string]interface{}

func GenerateDynamicConfig(routes []store.ProxyRoute, certStore *store.Store) (*DynamicConfig, error) {
	cfg := &DynamicConfig{
		HTTP: HTTPConfig{
			Routers:     make(map[string]RouterConfig),
			Services:    make(map[string]ServiceConfig),
			Middlewares: make(map[string]MiddlewareConfig),
		},
	}

	for _, route := range routes {
		routerName := sanitizeName(route.Name)
		serviceName := routerName + "-svc"

		router := RouterConfig{
			Rule:        fmt.Sprintf("Host(`%s`)", route.Domain),
			Service:     serviceName,
			EntryPoints: []string{"websecure"},
		}

		switch route.SSLMode {
		case "letsencrypt":
			router.TLS = &TLSRouterConfig{CertResolver: "letsencrypt"}
		case "cloudflare":
			router.TLS = &TLSRouterConfig{CertResolver: "cloudflare"}
		case "custom":
			router.TLS = &TLSRouterConfig{}
		default:
			router.TLS = &TLSRouterConfig{CertResolver: "letsencrypt"}
		}

		var mwConfig map[string]interface{}
		if len(route.MiddlewareConfig) > 0 {
			if err := json.Unmarshal(route.MiddlewareConfig, &mwConfig); err == nil && len(mwConfig) > 0 {
				mwName := routerName + "-mw"
				cfg.HTTP.Middlewares[mwName] = MiddlewareConfig(mwConfig)
				router.Middlewares = []string{mwName}
			}
		}

		cfg.HTTP.Routers[routerName] = router
		cfg.HTTP.Services[serviceName] = ServiceConfig{
			LoadBalancer: LBConfig{
				Servers: []ServerEntry{
					{URL: fmt.Sprintf("http://%s:%d", route.TargetService, route.TargetPort)},
				},
			},
		}
	}

	return cfg, nil
}

func WriteDynamicConfig(configDir string, cfg *DynamicConfig) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(filepath.Join(configDir, "dynamic.yml"), data, 0644)
}

func sanitizeName(name string) string {
	result := make([]byte, 0, len(name))
	for _, b := range []byte(name) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
			result = append(result, b)
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}
