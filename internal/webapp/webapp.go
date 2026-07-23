package webapp

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func Embedded() http.Handler {
	distribution, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(distribution, "index.html"); err != nil {
		return nil
	}
	return Handler(distribution)
}

func Handler(distribution fs.FS) http.Handler {
	if distribution == nil {
		return nil
	}
	files := http.FileServer(http.FS(distribution))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if name == "." || name == "" {
			name = "index.html"
		}
		if fs.ValidPath(name) {
			if info, err := fs.Stat(distribution, name); err == nil && !info.IsDir() {
				files.ServeHTTP(response, request)
				return
			}
		}
		if _, err := fs.Stat(distribution, "index.html"); err != nil {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", mime.TypeByExtension(".html"))
		contents, err := fs.ReadFile(distribution, "index.html")
		if err != nil {
			http.Error(response, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(contents)
	})
}
