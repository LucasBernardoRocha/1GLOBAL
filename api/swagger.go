package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// Swagger UI assets are vendored from swagger-ui-dist so the documentation
// works offline, without a CDN or npm at runtime.
//
//go:embed swaggerui/index.html swaggerui/swagger-ui.css swaggerui/swagger-ui-bundle.js swaggerui/favicon-32x32.png
var swaggerUI embed.FS

// SwaggerUI returns the embedded Swagger UI. Mount it at /docs/.
func SwaggerUI() http.Handler {
	assets, err := fs.Sub(swaggerUI, "swaggerui")
	if err != nil {
		panic("api: embedded Swagger UI assets: " + err.Error())
	}

	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assets are pinned at build time, so they can be cached by the browser.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
