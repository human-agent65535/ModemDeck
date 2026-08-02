package webapp

import (
	"encoding/json"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func Embedded(applicationVersion string) http.Handler {
	distribution, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(distribution, "index.html"); err != nil {
		return nil
	}
	return Handler(distribution, applicationVersion)
}

func Handler(distribution fs.FS, applicationVersion string) http.Handler {
	if distribution == nil {
		return nil
	}
	applicationVersion = normalizedVersion(applicationVersion)
	recoveryModule := staleAssetRecoveryModule(applicationVersion)
	files := http.FileServer(http.FS(distribution))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		if name == "index.html" {
			serveEntryDocument(response, request, distribution)
			return
		}
		if fs.ValidPath(name) {
			if info, err := fs.Stat(distribution, name); err == nil && !info.IsDir() {
				setCachePolicy(response, name)
				files.ServeHTTP(response, request)
				return
			}
		}
		if (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
			strings.HasPrefix(name, "assets/") {
			response.Header().Set("Cache-Control", "no-store")
			switch path.Ext(name) {
			case ".js":
				response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
				if request.Method == http.MethodGet {
					_, _ = response.Write([]byte(recoveryModule))
				}
				return
			case ".css":
				response.Header().Set("Content-Type", "text/css; charset=utf-8")
				return
			}
		}
		if strings.HasPrefix(name, "assets/") {
			response.Header().Set("Cache-Control", "no-store")
			http.NotFound(response, request)
			return
		}
		if _, err := fs.Stat(distribution, "index.html"); err != nil {
			http.NotFound(response, request)
			return
		}
		serveEntryDocument(response, request, distribution)
	})
}

func setCachePolicy(response http.ResponseWriter, name string) {
	switch {
	case strings.HasPrefix(name, "assets/"):
		response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		response.Header().Set("Cache-Control", "no-cache")
		if name == "modemdeck-build.json" {
			response.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
		}
	}
}

func serveEntryDocument(
	response http.ResponseWriter,
	request *http.Request,
	distribution fs.FS,
) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(response, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")
	response.Header().Set("Content-Type", mime.TypeByExtension(".html"))
	contents, err := fs.ReadFile(distribution, "index.html")
	if err != nil {
		http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write(contents)
	}
}

func normalizedVersion(value string) string {
	if normalized := strings.TrimSpace(value); normalized != "" {
		return normalized
	}
	return "dev"
}

func staleAssetRecoveryModule(applicationVersion string) string {
	encodedVersion, _ := json.Marshal(applicationVersion)
	return `globalThis.dispatchEvent(new CustomEvent("modemdeck:update-ready",{detail:{version:` + string(encodedVersion) + `}}));export{}`
}
