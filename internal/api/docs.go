package api

import (
	_ "embed"
	"net/http"
)

//go:embed docs/openapi.yaml
var openAPISpec []byte

// swaggerUI is a self-contained HTML page that loads Swagger UI from the
// jsDelivr CDN and initialises it with the spec served at /api/docs/openapi.yaml.
const swaggerUI = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>TinyDM API Docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; }
    #swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "/api/docs/openapi.yaml",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
      defaultModelsExpandDepth: 1,
      defaultModelExpandDepth: 2,
      displayRequestDuration: true,
      tryItOutEnabled: true,
      persistAuthorization: true,
    });
  </script>
</body>
</html>`

// serveSwaggerUI serves the Swagger UI HTML page.
// The global SecurityHeaders middleware sets a tight CSP that blocks external
// CDN resources; we override it here to allow the jsDelivr assets that
// Swagger UI requires. All other routes keep the strict policy.
func serveSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self' 'unsafe-inline' cdn.jsdelivr.net; "+
			"style-src 'self' 'unsafe-inline' cdn.jsdelivr.net; "+
			"img-src 'self' data:; "+
			"frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(swaggerUI))
}

// serveOpenAPISpec serves the raw openapi.yaml spec file.
func serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec)
}
