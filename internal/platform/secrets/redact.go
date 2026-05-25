package secrets

import (
	"regexp"
	"strings"
)

var (
	bearerTokenPattern   = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	awsKeyPattern        = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	gitHubTokenPattern   = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,255}`)
	privateKeyPattern    = regexp.MustCompile(`-----BEGIN[A-Z\s]*PRIVATE KEY-----[^-]*-----END[A-Z\s]*PRIVATE KEY-----`)
	passwordPattern      = regexp.MustCompile(`(?i)(["']?(?:password|passwd|pwd)["']?\s*[:=]\s*["']?)([^"'\s]+)`)
	apiKeyPattern        = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|apikey)["']?\s*[:=]\s*["']?)([A-Za-z0-9\-._~+/]{20,})`)
	secretPattern        = regexp.MustCompile(`(?i)(["']?secret["']?\s*[:=]\s*["']?)([A-Za-z0-9\-._~+/]{16,})`)
	tokenPattern         = regexp.MustCompile(`(?i)(["']?token["']?\s*[:=]\s*["']?)([A-Za-z0-9\-._~+/]{20,})`)
	authValuePattern     = regexp.MustCompile(`(?i)(["']?(?:auth|authorization)["']?\s*[:=]\s*(?:(?:Bearer|Basic)\s+)?["']?)([^"'\s,}]+)`)
	sensitiveKeyPattern  = regexp.MustCompile(`(?i)(["']?(?:[a-z0-9_-]*credential[a-z0-9_-]*|[a-z0-9_-]*secret[a-z0-9_-]*|(?:access|refresh)[_-]?token|private[_-]?key|encryption[_-]?key|signing[_-]?key|ssh[_-]?key|ssl[_-]?key)["']?\s*[:=]\s*["']?)([^"'\s,}]+)`)
	jwtPattern           = regexp.MustCompile(`eyJ[A-Za-z0-9\-_]+\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+`)
	slackTokenPattern    = regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}-[A-Za-z0-9]{24,32}`)
	stripeKeyPattern     = regexp.MustCompile(`(?:r|s)k_(live|test)_[0-9a-zA-Z]{24,99}`)
	dockerAuthPattern    = regexp.MustCompile(`(?i)("auth":\s*")[^"]+`)
	sshPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN\s+(?:RSA|DSA|EC|OPENSSH)\s+PRIVATE\s+KEY-----.*?-----END\s+(?:RSA|DSA|EC|OPENSSH)\s+PRIVATE\s+KEY-----`)
)

// Redact replaces secrets in text with "***".
func Redact(text string) string {
	if text == "" {
		return text
	}

	result := text
	result = bearerTokenPattern.ReplaceAllString(result, `${1}***`)
	result = awsKeyPattern.ReplaceAllString(result, "AKIA***")
	result = gitHubTokenPattern.ReplaceAllString(result, "gh*_***")
	result = privateKeyPattern.ReplaceAllString(result, "-----BEGIN PRIVATE KEY----- [REDACTED] -----END PRIVATE KEY-----")
	result = sshPrivateKeyPattern.ReplaceAllString(result, "-----BEGIN PRIVATE KEY----- [REDACTED] -----END PRIVATE KEY-----")
	result = passwordPattern.ReplaceAllString(result, `${1}***`)
	result = apiKeyPattern.ReplaceAllString(result, `${1}***`)
	result = secretPattern.ReplaceAllString(result, `${1}***`)
	result = authValuePattern.ReplaceAllString(result, `${1}***`)
	result = sensitiveKeyPattern.ReplaceAllString(result, `${1}***`)
	result = jwtPattern.ReplaceAllString(result, "eyJ***.eyJ***.***")
	result = slackTokenPattern.ReplaceAllString(result, "xox*-***")
	result = tokenPattern.ReplaceAllString(result, `${1}***`)
	result = stripeKeyPattern.ReplaceAllString(result, "*k_***_***")
	result = dockerAuthPattern.ReplaceAllString(result, `${1}***`)
	return result
}

// RedactMap recursively redacts secrets in map structures.
func RedactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		key := strings.ToLower(k)
		if isSecretKey(key) {
			result[k] = "***"
			continue
		}
		switch val := v.(type) {
		case string:
			result[k] = Redact(val)
		case map[string]any:
			result[k] = RedactMap(val)
		case []any:
			result[k] = redactSlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

func isSecretKey(key string) bool {
	secretKeys := []string{
		"password", "passwd", "pwd",
		"secret", "api_key", "apikey", "api-key",
		"token", "auth", "authorization",
		"private_key", "privatekey", "private-key",
		"credential", "credentials",
		"access_token", "refresh_token",
		"client_secret", "session_secret",
		"encryption_key", "signing_key",
		"ssh_key", "ssl_key",
	}
	for _, sk := range secretKeys {
		if strings.Contains(key, sk) {
			return true
		}
	}
	return false
}

func redactSlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case string:
			result[i] = Redact(val)
		case map[string]any:
			result[i] = RedactMap(val)
		case []any:
			result[i] = redactSlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// RedactHeaders redacts sensitive HTTP headers.
func RedactHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		key := strings.ToLower(k)
		if isAuthHeader(key) {
			result[k] = "***"
		} else {
			result[k] = Redact(v)
		}
	}
	return result
}

func isAuthHeader(header string) bool {
	header = strings.ToLower(strings.TrimSpace(header))
	authHeaders := []string{
		"authorization",
		"x-api-key",
		"x-auth-token",
		"x-access-token",
		"cookie",
		"set-cookie",
		"proxy-authorization",
	}
	for _, ah := range authHeaders {
		if header == ah {
			return true
		}
	}
	sensitiveFragments := []string{
		"api-key",
		"api_key",
		"apikey",
		"api-token",
		"api_token",
		"access-token",
		"access_token",
		"auth-token",
		"auth_token",
		"security-token",
		"security_token",
		"session-token",
		"session_token",
		"csrf-token",
		"csrf_token",
		"xsrf-token",
		"xsrf_token",
		"client-secret",
		"client_secret",
		"credential",
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(header, fragment) {
			return true
		}
	}
	return false
}
