package app

import (
	"net/http"
	"strings"
)

const browserContentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'none'; " +
	"connect-src 'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com; " +
	"font-src 'self' data:; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"frame-src https://*.firebaseapp.com https://accounts.google.com; " +
	"img-src 'self' data: blob: https://*.googleusercontent.com; " +
	"manifest-src 'self'; " +
	"media-src 'self' blob:; " +
	"object-src 'none'; " +
	"script-src 'self' https://apis.google.com; " +
	"style-src 'self' 'unsafe-inline'; " +
	"worker-src 'self' blob:"

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		headers := response.Header()
		headers.Set("Content-Security-Policy", browserContentSecurityPolicy)
		headers.Set("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")
		headers.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), microphone=(), payment=(), usb=()")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(request.URL.Path, "/api/") || strings.HasPrefix(request.URL.Path, "/v1/") {
			headers.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
}
