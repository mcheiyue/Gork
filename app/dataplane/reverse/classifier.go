package reverse

import "strings"

type ClassifyOptions struct {
	Payload any
}

func ClassifyResult(statusCode int, body string, _ ...ClassifyOptions) ResultCategory {
	if statusCode == 200 {
		return ResultCategorySuccess
	}
	if statusCode == 429 {
		return ResultCategoryRateLimited
	}
	if statusCode == 401 {
		return ResultCategoryAuthFailure
	}
	if statusCode == 400 && isInvalidCredentialsBody(body) {
		return ResultCategoryAuthFailure
	}
	if statusCode == 403 {
		if isInvalidCredentialsBody(body) {
			return ResultCategoryAuthFailure
		}
		text := strings.ToLower(body)
		if body != "" && (strings.Contains(text, "cf-challenge") || strings.Contains(text, "cloudflare")) {
			return ResultCategoryForbidden
		}
		return ResultCategoryForbidden
	}
	if statusCode == 404 {
		return ResultCategoryNotFound
	}
	if statusCode >= 500 {
		return ResultCategoryUpstream5xx
	}
	return ResultCategoryUnknown
}

func isInvalidCredentialsBody(body string) bool {
	text := strings.ToLower(body)
	return strings.Contains(text, "invalid-credentials") ||
		strings.Contains(text, "bad-credentials") ||
		strings.Contains(text, "failed to look up session id") ||
		strings.Contains(text, "blocked-user") ||
		strings.Contains(text, "email-domain-rejected") ||
		strings.Contains(text, "session not found") ||
		strings.Contains(text, "account suspended") ||
		strings.Contains(text, "token revoked") ||
		strings.Contains(text, "token expired")
}
