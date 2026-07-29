package httpapi

import "net/http"

func (api *API) setSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set(
		"Content-Security-Policy",
		"default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self' data:; object-src 'none'; script-src 'self' https://accounts.google.com/gsi/client; style-src 'self'; connect-src 'self' https://accounts.google.com/gsi/ https://people.googleapis.com; frame-src https://accounts.google.com/gsi/",
	)
	response.Header().Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
	response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=(self)")
	response.Header().Set("Referrer-Policy", "same-origin")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
	if api.secureCookies {
		response.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
}
