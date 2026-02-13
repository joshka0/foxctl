// Package main implements the code/greenlight skill.
//
// This skill ports the greenlight codescan logic into an agentctl skill.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	skillfs "github.com/jkatigb/agentctl/internal/adapters/skillslib/fs"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "code/greenlight"

const (
	defaultScanMode      = "scan"
	defaultSeverityMin   = "medium"
	defaultMaxResults    = 200
	maxReadableFileBytes = 4 * 1024 * 1024
)

type input struct {
	Path              string   `json:"path"`
	ScanMode          string   `json:"scan_mode"`
	SeverityThreshold string   `json:"severity_threshold"`
	Categories        []string `json:"categories"`
	ExcludeTests      bool     `json:"exclude_tests"`
	MaxResults        int      `json:"max_results"`
}

type finding struct {
	RuleID    string `json:"rule_id"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Guideline string `json:"guideline"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Fix       string `json:"fix,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Code      string `json:"code,omitempty"`
}

type fileContext struct {
	Path     string
	RelPath  string
	Lines    []string
	Language string
}

type Rule interface {
	Applies(fc fileContext) bool
	Check(fc fileContext) []finding
	RuleID() string
	Category() string
}

type GlobalAntiPatternRule interface {
	Rule
	HasGlobalAntiPatterns() bool
	AntiPatternMatched(fc fileContext) bool
}

type patternRule struct {
	id                 string
	category           string
	guideline          string
	severity           string
	title              string
	detail             string
	fix                string
	languages          []string
	patterns           []*regexp.Regexp
	antiPatterns       []*regexp.Regexp
	antiPatternsGlobal bool
	ignorePatterns     []*regexp.Regexp
	countThreshold     int
}

type plistKeyRule struct {
	id        string
	category  string
	guideline string
}

type expoConfigRule struct {
	id string
}

var emptyPurposePatterns = map[string]*regexp.Regexp{}

func init() {
	emptyPurposePatterns = map[string]*regexp.Regexp{
		"NSCameraUsageDescription":            mustCompile(`NSCameraUsageDescription</key>\s*<string>\s*</string>`),
		"NSMicrophoneUsageDescription":        mustCompile(`NSMicrophoneUsageDescription</key>\s*<string>\s*</string>`),
		"NSPhotoLibraryUsageDescription":      mustCompile(`NSPhotoLibraryUsageDescription</key>\s*<string>\s*</string>`),
		"NSLocationWhenInUseUsageDescription": mustCompile(`NSLocationWhenInUseUsageDescription</key>\s*<string>\s*</string>`),
		"NSLocationAlwaysUsageDescription":    mustCompile(`NSLocationAlwaysUsageDescription</key>\s*<string>\s*</string>`),
		"NSBluetoothAlwaysUsageDescription":   mustCompile(`NSBluetoothAlwaysUsageDescription</key>\s*<string>\s*</string>`),
		"NSMotionUsageDescription":            mustCompile(`NSMotionUsageDescription</key>\s*<string>\s*</string>`),
		"NSFaceIDUsageDescription":            mustCompile(`NSFaceIDUsageDescription</key>\s*<string>\s*</string>`),
		"NSUserTrackingUsageDescription":      mustCompile(`NSUserTrackingUsageDescription</key>\s*<string>\s*</string>`),
	}
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func allRules() []Rule {
	return []Rule{
		&patternRule{
			id:        "private-api",
			category:  "private_api",
			title:     "Private API usage detected",
			guideline: "2.5.1",
			severity:  "critical",
			detail:    "Using private/undocumented Apple APIs will cause immediate rejection.",
			fix:       "Replace with public API equivalents.",
			languages: []string{"swift", "objc"},
			patterns: []*regexp.Regexp{
				mustCompile(`NSSelectorFromString\s*\(\s*"_`),
				mustCompile(`performSelector.*"_`),
				mustCompile(`dlopen\s*\(`),
				mustCompile(`dlsym\s*\(`),
			},
		},
		&patternRule{
			id:        "hardcoded-secrets",
			category:  "hardcoded-secrets",
			title:     "Hardcoded secret/API key detected",
			guideline: "1.6",
			severity:  "critical",
			detail:    "Hardcoded secrets in source code is a security vulnerability and review risk.",
			fix:       "Move secrets to environment variables or a secure keychain.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(sk_live_|sk_test_|pk_live_|pk_test_)[a-zA-Z0-9]{20,}`),
				mustCompile(`(?i)(api[_-]?key|api[_-]?secret|secret[_-]?key)\s*[:=]\s*[\"'][a-zA-Z0-9]{20,}[\"']`),
				mustCompile(`(?i)AKIA[0-9A-Z]{16}`),
				mustCompile(`(?i)ghp_[a-zA-Z0-9]{36}`),
			},
		},
		&patternRule{
			id:        "external-payment-digital",
			category:  "payment",
			title:     "External payment for potentially digital goods",
			guideline: "3.1.1",
			severity:  "critical",
			detail:    "Using Stripe/PayPal/external payments for digital goods violates IAP requirements. Physical goods are OK.",
			fix:       "Use StoreKit/IAP for digital goods. External payment is only allowed for physical goods and services.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)stripe.*payment.*intent`),
				mustCompile(`(?i)paypal.*checkout`),
				mustCompile(`(?i)braintree.*payment`),
				mustCompile(`(?i)checkout\.redirect.*url`),
			},
		},
		&patternRule{
			id:        "crypto-mining",
			category:  "crypto",
			title:     "Cryptocurrency mining detected",
			guideline: "3.1.5",
			severity:  "critical",
			detail:    "On-device cryptocurrency mining is explicitly prohibited.",
			fix:       "Remove all mining functionality.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(crypto|coin)\s*miner`),
				mustCompile(`(?i)hash\s*rate`),
				mustCompile(`(?i)mining\s*pool`),
				mustCompile(`(?i)stratum\+tcp`),
			},
		},
		&patternRule{
			id:        "dynamic-code-exec",
			category:  "code-execution",
			title:     "Dynamic code execution detected",
			guideline: "2.5.2",
			severity:  "critical",
			detail:    "Apps may not download, install, or execute code that changes app behavior.",
			fix:       "Remove dynamic code execution. Use native APIs instead.",
			languages: []string{"swift", "objc"},
			patterns: []*regexp.Regexp{
				mustCompile(`JSContext\s*\(\s*\).*evaluateScript`),
				mustCompile(`dlopen\s*\(`),
				mustCompile(`NSBundle.*load\b`),
			},
		},

		&patternRule{
			id:        "missing-att",
			category:  "tracking",
			title:     "Ad/tracking SDK without ATT implementation",
			guideline: "5.1.2",
			severity:  "warn",
			detail:    "Using advertising or tracking SDKs requires App Tracking Transparency.",
			fix:       "Implement ATT prompt before any tracking. Add NSUserTrackingUsageDescription to Info.plist.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(firebase.*analytics|google.*analytics|facebook.*sdk|fbsdk|adjust.*sdk|appsflyer|amplitude|mixpanel)`),
				mustCompile(`(?i)(import.*@segment/|analytics-react-native|SegmentAnalytics|createClient.*writeKey)`),
			},
			antiPatterns: []*regexp.Regexp{
				mustCompile(`(?i)(ATTrackingManager|requestTrackingAuthorization|AppTrackingTransparency|expo-tracking-transparency)`),
			},
			antiPatternsGlobal: true,
		},
		&patternRule{
			id:        "social-login-no-apple",
			category:  "auth",
			title:     "Social login without Sign in with Apple",
			guideline: "4.8",
			severity:  "warn",
			detail:    "Apps with third-party login (Google, Facebook, etc.) must also offer Sign in with Apple.",
			fix:       "Add Sign in with Apple as a login option alongside other social logins.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(google.*sign.*in|GIDSignIn|GoogleSignin|facebook.*login|FBSDKLoginManager|LoginManager\.logIn)`),
			},
			antiPatterns: []*regexp.Regexp{
				mustCompile(`(?i)(ASAuthorizationAppleIDProvider|SignInWithApple|apple.*auth|appleAuth|expo-apple-authentication)`),
			},
			antiPatternsGlobal: true,
		},
		&patternRule{
			id:        "iap-no-restore",
			category:  "purchase",
			title:     "In-app purchases without restore functionality",
			guideline: "3.1.1",
			severity:  "warn",
			detail:    "Apps with IAP must include a 'Restore Purchases' button.",
			fix:       "Add a 'Restore Purchases' button that calls restoreCompletedTransactions or equivalent.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(SKPaymentQueue|StoreKit|Product\.purchase|purchaseProduct|expo-in-app-purchases|react-native-iap|RevenueCat)`),
			},
			antiPatterns: []*regexp.Regexp{
				mustCompile(`(?i)(restoreCompletedTransactions|restore.*purchase|restorePurchase|customerInfo|syncPurchases)`),
			},
			antiPatternsGlobal: true,
		},
		&patternRule{
			id:        "account-no-delete",
			category:  "account",
			title:     "Account creation without account deletion",
			guideline: "5.1.1",
			severity:  "warn",
			detail:    "Apps that allow account creation must also offer account deletion functionality.",
			fix:       "Add an account deletion option in settings. Must actually delete data, not just deactivate.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(createAccount|signUp|register.*user|create.*account|auth\(\)\.createUser)`),
			},
			antiPatterns: []*regexp.Regexp{
				mustCompile(`(?i)(deleteAccount|delete.*account|remove.*account|account.*delet)`),
			},
			antiPatternsGlobal: true,
		},

		&patternRule{
			id:        "platform-reference",
			category:  "content",
			title:     "Reference to competing platform",
			guideline: "2.3",
			severity:  "warn",
			detail:    "Mentioning other platforms (Android, Google Play, etc.) in user-facing strings may cause rejection.",
			fix:       "Remove references to competing platforms from all user-visible text.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)"[^"]*\b(android|google\s*play|play\s*store|samsung|windows\s*phone)\b[^\"]*"`),
				mustCompile(`(?i)'[^']*\b(android|google\s*play|play\s*store|samsung|windows\s*phone)\b[^']*'`),
				mustCompile("(?i)`[^`]*\\b(android|google\\s*play|play\\s*store|samsung|windows\\s*phone)\\b[^`]*`"),
				mustCompile(`(?i)\b(android|google\s*play|play\s*store|samsung|windows\s*phone)\b`),
			},
		},
		&patternRule{
			id:        "placeholder-content",
			category:  "content",
			title:     "Placeholder content in user-facing strings",
			guideline: "2.1",
			severity:  "warn",
			detail:    "Placeholder text will cause rejection under App Completeness guidelines.",
			fix:       "Replace all placeholder text with final content.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)"[^"]*\b(lorem ipsum|placeholder|coming soon|under construction|todo|tbd)\b[^\"]*"`),
				mustCompile(`(?i)'[^']*\b(lorem ipsum|placeholder|coming soon|under construction|todo|tbd)\b[^']*'`),
				mustCompile("(?i)`[^`]*\\b(lorem ipsum|placeholder|coming soon|under construction|todo|tbd)\\b[^`]*`"),
				mustCompile(`(?i)\b(lorem ipsum|placeholder|coming soon|under construction|todo|tbd)\b`),
			},
		},
		&patternRule{
			id:        "console-log",
			category:  "quality",
			title:     "Debug logging in production code",
			guideline: "2.1",
			severity:  "info",
			detail:    "Debug logging in production code may indicate the app is not production-ready.",
			fix:       "Remove or gate debug logging behind a DEBUG flag.",
			languages: []string{"typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`console\.(log|debug|warn|error)\s*\(`),
			},
			countThreshold: 5,
		},
		&patternRule{
			id:        "hardcoded-ipv4",
			category:  "network",
			title:     "Hardcoded IPv4 address",
			guideline: "2.5",
			severity:  "warn",
			detail:    "Apps must support IPv6. Hardcoded IPv4 addresses will fail on IPv6-only networks.",
			fix:       "Use hostnames instead of IP addresses. Ensure all networking supports IPv6.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
			},
			ignorePatterns: []*regexp.Regexp{
				mustCompile(`(?i)(version|0\.0\.0|127\.0\.0\.1|localhost)`),
			},
		},
		&patternRule{
			id:        "http-not-https",
			category:  "network",
			title:     "Insecure HTTP URL",
			guideline: "1.6",
			severity:  "warn",
			detail:    "App Transport Security requires HTTPS. HTTP URLs will be blocked by default.",
			fix:       "Use HTTPS for all network requests.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`"http://[^"]+"`),
				mustCompile(`'http://[^']+'`),
			},
			ignorePatterns: []*regexp.Regexp{
				mustCompile(`(?i)(localhost|127\.0\.0\.1|0\.0\.0\.0|http://example)`),
			},
		},
		&patternRule{
			id:        "webview-only",
			category:  "functionality",
			title:     "WebView-only app pattern detected",
			guideline: "4.2",
			severity:  "warn",
			detail:    "Apps that are primarily WebView wrappers may be rejected for minimum functionality.",
			fix:       "Add native features beyond just loading a web page.",
			languages: []string{"swift", "objc", "typescript", "javascript"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)(WKWebView|UIWebView|WebView|react-native-webview).*loadRequest.*https?://`),
			},
		},
		&patternRule{
			id:        "vague-purpose-string",
			category:  "privacy",
			title:     "Vague permission purpose string",
			guideline: "5.1.1",
			severity:  "warn",
			detail:    "Purpose strings must clearly explain why the app needs the permission. Vague strings get rejected.",
			fix:       "Write specific purpose strings: 'Take photos to attach to support tickets' NOT 'Camera access needed'.",
			languages: []string{"plist"},
			patterns: []*regexp.Regexp{
				mustCompile(`(?i)<string>\s*(camera access|location access|microphone access|photo access|this app (needs|requires|uses))\s*</string>`),
				mustCompile(`(?i)<string>\s*(needed|required|for the app|to function|for functionality)\s*\.?\s*</string>`),
			},
		},
		&plistKeyRule{
			id:        "missing-privacy-keys",
			category:  "privacy",
			guideline: "5.1.1",
		},
		&expoConfigRule{id: "expo-config-check"},
	}
}

func (r *patternRule) RuleID() string   { return r.id }
func (r *patternRule) Category() string { return r.category }

func (r *patternRule) HasGlobalAntiPatterns() bool {
	return r.antiPatternsGlobal && len(r.antiPatterns) > 0
}

func (r *patternRule) AntiPatternMatched(fc fileContext) bool {
	for _, line := range fc.Lines {
		for _, ap := range r.antiPatterns {
			if ap.MatchString(line) {
				return true
			}
		}
	}
	return false
}

func (r *patternRule) Applies(fc fileContext) bool {
	for _, lang := range r.languages {
		if fc.Language == lang {
			return true
		}
	}
	return false
}

func (r *patternRule) Check(fc fileContext) []finding {
	var findings []finding

	for lineNum, line := range fc.Lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		skipped := false
		for _, p := range r.ignorePatterns {
			if p.MatchString(line) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}

		for _, p := range r.patterns {
			if p.MatchString(line) {
				findings = append(findings, finding{
					RuleID:    r.id,
					Category:  r.category,
					Severity:  r.severity,
					Guideline: r.guideline,
					Title:     r.title,
					Detail:    r.detail,
					Fix:       r.fix,
					File:      fc.RelPath,
					Line:      lineNum + 1,
					Code:      strings.TrimSpace(line),
				})
				break
			}
		}
	}

	if r.countThreshold > 0 && len(findings) <= r.countThreshold {
		return nil
	}

	return findings
}

func (r *plistKeyRule) RuleID() string   { return r.id }
func (r *plistKeyRule) Category() string { return r.category }

func (r *plistKeyRule) Applies(fc fileContext) bool {
	return fc.Language == "plist" && strings.HasSuffix(strings.ToLower(fc.RelPath), "info.plist")
}

func (r *plistKeyRule) Check(fc fileContext) []finding {
	content := strings.Join(fc.Lines, "\n")
	requiredIfUsed := map[string]string{
		"NSCameraUsageDescription":            "Camera",
		"NSMicrophoneUsageDescription":        "Microphone",
		"NSPhotoLibraryUsageDescription":      "Photo Library",
		"NSLocationWhenInUseUsageDescription": "Location (When In Use)",
		"NSLocationAlwaysUsageDescription":    "Location (Always)",
		"NSBluetoothAlwaysUsageDescription":   "Bluetooth",
		"NSMotionUsageDescription":            "Motion/Accelerometer",
		"NSFaceIDUsageDescription":            "Face ID",
		"NSUserTrackingUsageDescription":      "App Tracking",
	}

	var findings []finding
	for key, name := range requiredIfUsed {
		if strings.Contains(content, key) {
			if re, ok := emptyPurposePatterns[key]; ok && re.MatchString(content) {
				findings = append(findings, finding{
					RuleID:    r.id,
					Category:  r.category,
					Severity:  "warn",
					Guideline: r.guideline,
					Title:     name + " purpose string is empty",
					Detail:    "The " + key + " key exists but has no description.",
					Fix:       "Add a clear, specific description of why your app needs " + name + " access.",
					File:      fc.RelPath,
				})
			}
		}
	}

	return findings
}

func (r *expoConfigRule) RuleID() string   { return r.id }
func (r *expoConfigRule) Category() string { return "metadata" }

func (r *expoConfigRule) Applies(fc fileContext) bool {
	base := strings.ToLower(filepath.Base(fc.Path))
	return base == "app.json" || strings.HasPrefix(base, "app.config.")
}

func (r *expoConfigRule) Check(fc fileContext) []finding {
	content := strings.Join(fc.Lines, "\n")
	lower := strings.ToLower(content)

	var findings []finding
	if !strings.Contains(content, `"expo"`) {
		return findings
	}

	if !strings.Contains(content, `"bundleIdentifier"`) {
		findings = append(findings, finding{
			RuleID:    r.id,
			Category:  "metadata",
			Severity:  "warn",
			Guideline: "2.1",
			Title:     "Missing iOS bundle identifier in Expo config",
			Detail:    "The expo.ios.bundleIdentifier is not set.",
			Fix:       "Add bundleIdentifier to the ios section of your app.json.",
			File:      fc.RelPath,
		})
	}

	if !strings.Contains(content, `"icon"`) {
		findings = append(findings, finding{
			RuleID:    r.id,
			Category:  "metadata",
			Severity:  "warn",
			Guideline: "2.3",
			Title:     "Missing app icon in Expo config",
			Detail:    "No icon field found in app.json.",
			Fix:       "Add an icon field pointing to a 1024x1024 PNG.",
			File:      fc.RelPath,
		})
	}

	if strings.Contains(lower, `"my app"`) || strings.Contains(lower, `"new app"`) || strings.Contains(lower, `"test app"`) {
		findings = append(findings, finding{
			RuleID:    r.id,
			Category:  "metadata",
			Severity:  "warn",
			Guideline: "2.1",
			Title:     "Placeholder app name detected",
			Detail:    "The app name looks like a placeholder.",
			Fix:       "Set a proper app name before submitting.",
			File:      fc.RelPath,
		})
	}

	return findings
}

// main is the skill entrypoint.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates greenlight-style scanning and artifact-aware output.
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	if strings.TrimSpace(in.ScanMode) == "" {
		in.ScanMode = defaultScanMode
	}
	if strings.TrimSpace(in.SeverityThreshold) == "" {
		in.SeverityThreshold = defaultSeverityMin
	}
	if in.MaxResults <= 0 {
		in.MaxResults = defaultMaxResults
	}

	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	rules, categoriesUsed, err := selectRules(in)
	if err != nil {
		return err
	}

	files, err := collectFiles(searchPath, workspace, in)
	if err != nil {
		return err
	}

	suppressed := map[string]bool{}
	for _, rule := range rules {
		gar, ok := rule.(GlobalAntiPatternRule)
		if !ok || !gar.HasGlobalAntiPatterns() {
			continue
		}
		for _, fc := range files {
			if !rule.Applies(fc) {
				continue
			}
			if gar.AntiPatternMatched(fc) {
				suppressed[gar.RuleID()] = true
				break
			}
		}
	}

	var findings []finding
	for _, fc := range files {
		for _, rule := range rules {
			if !rule.Applies(fc) {
				continue
			}
			if gar, ok := rule.(GlobalAntiPatternRule); ok && gar.HasGlobalAntiPatterns() {
				if suppressed[gar.RuleID()] {
					continue
				}
			}
			findings = append(findings, rule.Check(fc)...)
		}
	}

	severity := normalizeSeverity(strings.TrimSpace(strings.ToLower(in.SeverityThreshold)))
	findings = filterBySeverity(findings, severity)
	findings = sortFindings(findings)

	if in.MaxResults > 0 && in.MaxResults < len(findings) {
		findings = findings[:in.MaxResults]
	}

	stats := computeStats(findings)
	stats["scan_categories"] = categoriesUsed
	stats["files_scanned"] = len(files)

	preview, err := skillout.PreviewAndPersistNDJSON(ctx, rc, findings, rc.MaxPreview, "code_greenlight", true)
	if err != nil {
		return err
	}

	data := map[string]any{
		"scan_mode":          strings.TrimSpace(strings.ToLower(in.ScanMode)),
		"severity_threshold": severity,
		"findings_count":     len(findings),
		"findings":           preview.Preview,
		"stats":              stats,
		"max_results":        in.MaxResults,
		"truncated":          preview.Truncated,
	}
	skillout.AddArtifact(data, preview.Artifact)

	return skillout.Emit(rc, command, data)
}

func selectRules(in input) ([]Rule, []string, error) {
	mode := strings.ToLower(strings.TrimSpace(in.ScanMode))
	categories := normalizeTokens(in.Categories)

	switch mode {
	case "secrets":
		categories = append(categories, "hardcoded-secrets")
	case "crypto":
		categories = append(categories, "crypto")
	case "injection":
		categories = append(categories, "code-execution")
	}

	all := allRules()
	if len(categories) == 0 {
		return all, categories, nil
	}

	selected := make([]Rule, 0, len(all))
	for _, rule := range all {
		id := strings.ToLower(strings.TrimSpace(rule.RuleID()))
		cat := strings.ToLower(strings.TrimSpace(rule.Category()))
		if contains(categories, id) || contains(categories, cat) {
			selected = append(selected, rule)
		}
	}

	if len(selected) == 0 {
		return all, categories, nil
	}
	return selected, categories, nil
}

func collectFiles(root, workspace string, in input) ([]fileContext, error) {
	excludes := []string{
		"Pods",
		"build",
		"dist",
		"vendor",
		"DerivedData",
		".next",
		"target",
		".cache",
		".expo",
		"coverage",
		"tmp",
	}
	excludes = append(excludes, skillfs.AppendCommonExcludes(nil)...)

	entries, err := skillfs.CollectEntries(skillfs.CollectOptions{
		Paths:         []string{root},
		Exclude:       excludes,
		IncludeHidden: false,
	})
	if err != nil {
		return nil, err
	}

	var files []fileContext
	for _, entry := range entries {
		if in.ExcludeTests && fsutil.IsTestFile(entry.Info.Name()) {
			continue
		}
		if entry.Info.Size() > maxReadableFileBytes {
			continue
		}
		if fsutil.IsBinaryFile(entry.Path) {
			continue
		}

		lang := detectLanguage(entry.Path)
		if lang == "" {
			continue
		}

		lines, err := readLines(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Path, err)
		}
		if isBinary(lines) {
			continue
		}

		files = append(files, fileContext{
			Path:     entry.Path,
			RelPath:  pathutil.RelTo(workspace, entry.Path),
			Language: lang,
			Lines:    lines,
		})
	}

	return files, nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	switch ext {
	case ".swift":
		return "swift"
	case ".m", ".h":
		return "objc"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".plist", ".entitlements", ".xcprivacy":
		return "plist"
	case ".json":
		return "json"
	}

	switch base {
	case "package.json", "app.json", "app.config.js", "app.config.ts", "app.config.mjs", "info.plist", "privacyinfo.xcprivacy":
		return "json"
	}
	if strings.HasSuffix(base, ".plist") || strings.HasSuffix(base, "info.plist") {
		return "plist"
	}
	if strings.HasSuffix(base, ".json") {
		return "json"
	}
	return ""
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func isBinary(lines []string) bool {
	for _, line := range lines {
		for _, b := range []byte(line) {
			if b == 0 {
				return true
			}
		}
	}
	return false
}

func contains(values []string, item string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), item) {
			return true
		}
	}
	return false
}

func normalizeTokens(values []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, item := range values {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "low", "info":
		return "info"
	case "medium", "warn", "high":
		return "warn"
	case "critical":
		return "critical"
	default:
		return "warn"
	}
}

func filterBySeverity(items []finding, threshold string) []finding {
	min := severityScore(threshold)
	filtered := make([]finding, 0, len(items))
	for _, item := range items {
		if severityScore(item.Severity) >= min {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func severityScore(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "info", "low":
		return 1
	case "warn", "medium", "high":
		return 2
	case "critical":
		return 3
	default:
		return 1
	}
}

func computeStats(items []finding) map[string]any {
	severity := map[string]int{}
	category := map[string]int{}
	for _, item := range items {
		severity[item.Severity]++
		category[item.Category]++
	}
	return map[string]any{
		"total":        len(items),
		"by_severity":  severity,
		"by_category":  category,
		"has_critical": severity["critical"] > 0,
	}
}

func sortFindings(items []finding) []finding {
	sort.Slice(items, func(i, j int) bool {
		si := severityScore(items[i].Severity)
		sj := severityScore(items[j].Severity)
		if si != sj {
			return si > sj
		}
		if items[i].File != items[j].File {
			return items[i].File < items[j].File
		}
		return items[i].Line < items[j].Line
	})
	return items
}
