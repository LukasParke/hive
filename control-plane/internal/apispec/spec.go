// Package apispec embeds the OpenAPI specification so the control-plane binary
// can serve it (and a Scalar-powered reference UI) without touching the
// filesystem at runtime.
//
// The embedded openapi.yaml is generated from api/openapi.yaml — the single
// source of truth. Run `make apispec` to refresh it; CI fails if it drifts.
package apispec

import (
	_ "embed"
	"net/http"
)

// OpenAPI holds the generated OpenAPI schema document.
//go:embed openapi.yaml
var OpenAPI []byte

// Spec serves the embedded OpenAPI document as YAML.
func Spec() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(OpenAPI)
	}
}

// docsPage is Scalar's recommended standalone HTML embed, pointing the
// reference UI at the spec served by Spec.
const docsPage = `<!doctype html>
<html>
  <head>
    <title>Hive API Reference</title>
    <meta charset="utf-8" />
    <meta
      name="viewport"
      content="width=device-width, initial-scale=1" />
  </head>

  <body>
    <div id="app"></div>

    <!-- Load the Script -->
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>

    <!-- Initialize the Scalar API Reference -->
    <script>
      Scalar.createApiReference('#app', {
        // The URL of the OpenAPI document, served by this control plane.
        url: '/api/v1/openapi.yaml',
      })
    </script>
  </body>
</html>`

// Docs serves the Scalar API Reference page.
func Docs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(docsPage))
	}
}
