package email

import (
	"bytes"
	"html"
	"net/mail"
	"regexp"
	"strings"

	"ai-email-responder/config"

	golangHTML "golang.org/x/net/html"
)

var (
	reSubjectPrefix = regexp.MustCompile(`^(?i)(re:\s*)+`)
	reMultiNewlines = regexp.MustCompile(`\n{3,}`)
	reWhitespace    = regexp.MustCompile(`[ \t]+`)
)

// ExtractEmailAddress parses an RFC 5322 address string and returns the pure email address in lowercase.
// For example, "John Doe <John@Example.COM>" becomes "john@example.com".
func ExtractEmailAddress(addressStr string) string {
	addressStr = strings.TrimSpace(addressStr)
	if addressStr == "" {
		return ""
	}

	addr, err := mail.ParseAddress(addressStr)
	if err == nil && addr.Address != "" {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}

	// Fallback regex in case of poorly formatted header
	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	match := emailRegex.FindString(addressStr)
	if match != "" {
		return strings.ToLower(match)
	}

	return strings.ToLower(addressStr)
}

// FormatReplySubject ensures the subject starts with a single "Re: " prefix without chaining "Re: Re: ".
func FormatReplySubject(origSubject string) string {
	trimmed := strings.TrimSpace(origSubject)
	if trimmed == "" {
		return "Re: (No Subject)"
	}

	// Strip any existing repeated Re: or RE: prefixes
	cleaned := reSubjectPrefix.ReplaceAllString(trimmed, "")
	cleaned = strings.TrimSpace(cleaned)

	return "Re: " + cleaned
}

// DetermineRecipient decides where the reply should be sent, prioritizing Reply-To over From.
func DetermineRecipient(msg *Message) string {
	if msg == nil {
		return ""
	}

	if msg.ReplyTo != "" {
		parsedReplyTo := ExtractEmailAddress(msg.ReplyTo)
		if parsedReplyTo != "" {
			return parsedReplyTo
		}
	}

	if msg.FromEmail != "" {
		return msg.FromEmail
	}

	return ExtractEmailAddress(msg.From)
}

// HTMLToPlainText converts HTML markup into clean, readable plain text.
// It strips script and style tags, formats paragraph/line breaks, and unescapes entities.
func HTMLToPlainText(htmlContent string) string {
	doc, err := golangHTML.Parse(strings.NewReader(htmlContent))
	if err != nil {
		// Fallback simple tag stripper if HTML parser fails
		tagRegex := regexp.MustCompile(`<[^>]*>`)
		return strings.TrimSpace(tagRegex.ReplaceAllString(htmlContent, " "))
	}

	var buf bytes.Buffer
	var extractText func(*golangHTML.Node)

	extractText = func(n *golangHTML.Node) {
		if n == nil {
			return
		}

		// Discard unwanted tags and their children
		if n.Type == golangHTML.ElementNode {
			switch strings.ToLower(n.Data) {
			case "script", "style", "noscript", "head", "iframe":
				return
			}
		}

		if n.Type == golangHTML.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				buf.WriteString(text)
				buf.WriteString(" ")
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}

		// Insert newlines on block elements
		if n.Type == golangHTML.ElementNode {
			switch strings.ToLower(n.Data) {
			case "p", "div", "br", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "hr":
				buf.WriteString("\n")
			}
		}
	}

	extractText(doc)

	// Clean up resulting text
	res := html.UnescapeString(buf.String())
	lines := strings.Split(res, "\n")
	var cleanedLines []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(reWhitespace.ReplaceAllString(l, " "))
		if trimmed != "" {
			cleanedLines = append(cleanedLines, trimmed)
		}
	}

	cleanedText := strings.Join(cleanedLines, "\n\n")
	return strings.TrimSpace(reMultiNewlines.ReplaceAllString(cleanedText, "\n\n"))
}

// SanitizeAndTruncateBody prepares the email body for AI consumption.
// It converts HTML to text if necessary, limits length to maxBodyLength, and reports if truncated.
func SanitizeAndTruncateBody(body string, isHTML bool, maxBodyLength int) (string, bool) {
	if maxBodyLength <= 0 {
		maxBodyLength = 10000
	}

	text := strings.TrimSpace(body)
	if isHTML || (strings.Contains(strings.ToLower(text), "<html") || strings.Contains(strings.ToLower(text), "<div")) {
		text = HTMLToPlainText(text)
	}

	runes := []rune(text)
	if len(runes) > maxBodyLength {
		truncated := string(runes[:maxBodyLength]) + "\n\n... [Email truncated due to length limits]"
		return truncated, true
	}

	return text, false
}

// CheckFilters evaluates an incoming email against safety and anti-loop rules.
func CheckFilters(msg *Message, myEmail string, ignoreOwn, ignoreNoReply, ignoreAutomated bool) FilterResult {
	if msg == nil {
		return FilterResult{ShouldIgnore: true, Reason: "nil message"}
	}

	cleanSender := ExtractEmailAddress(msg.From)
	cleanMyEmail := strings.ToLower(strings.TrimSpace(myEmail))

	// 1. Ignore own emails
	if ignoreOwn && cleanMyEmail != "" && cleanSender == cleanMyEmail {
		return FilterResult{ShouldIgnore: true, Reason: "sender is the application's own email address"}
	}

	// 2. Ignore no-reply variants
	if ignoreNoReply {
		noReplyPatterns := []string{"no-reply", "noreply", "do-not-reply", "donotreply"}
		for _, pattern := range noReplyPatterns {
			if strings.Contains(cleanSender, pattern) {
				return FilterResult{ShouldIgnore: true, Reason: "sender matches no-reply pattern (" + pattern + ")"}
			}
		}
	}

	// 3. Ignore automated emails and bounces
	if ignoreAutomated {
		// Delivery failure and bounce detection
		bounceSenders := []string{"mailer-daemon", "postmaster", "mail delivery subsystem"}
		for _, bs := range bounceSenders {
			if strings.Contains(cleanSender, bs) {
				return FilterResult{ShouldIgnore: true, Reason: "bounce or mailer-daemon sender"}
			}
		}

		bounceSubjects := []string{
			"delivery status notification",
			"undelivered mail returned to sender",
			"failure notice",
			"mail delivery failed",
			"returned mail: see transcript for details",
		}
		lowerSubject := strings.ToLower(msg.Subject)
		for _, bs := range bounceSubjects {
			if strings.Contains(lowerSubject, bs) {
				return FilterResult{ShouldIgnore: true, Reason: "subject indicates delivery failure or bounce"}
			}
		}

		// Header checks
		if getHeader(msg.Headers, "Auto-Submitted") != "" {
			val := strings.ToLower(getHeader(msg.Headers, "Auto-Submitted"))
			if val != "no" && val != "" {
				return FilterResult{ShouldIgnore: true, Reason: "header Auto-Submitted: " + val}
			}
		}

		if getHeader(msg.Headers, "X-Auto-Response-Suppress") != "" {
			return FilterResult{ShouldIgnore: true, Reason: "header X-Auto-Response-Suppress is present"}
		}

		if getHeader(msg.Headers, "Precedence") != "" {
			val := strings.ToLower(getHeader(msg.Headers, "Precedence"))
			if val == "bulk" || val == "list" || val == "junk" {
				return FilterResult{ShouldIgnore: true, Reason: "header Precedence: " + val}
			}
		}

		if getHeader(msg.Headers, "List-Id") != "" || getHeader(msg.Headers, "List-Unsubscribe") != "" {
			return FilterResult{ShouldIgnore: true, Reason: "mailing list or newsletter header detected"}
		}
	}

	return FilterResult{ShouldIgnore: false, Reason: ""}
}

func getHeader(headers map[string][]string, key string) string {
	if headers == nil {
		return ""
	}
	for k, v := range headers {
		if strings.EqualFold(k, key) && len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
	}
	return ""
}

var (
	rePlaceholderName    = regexp.MustCompile(`(?i)\[\s*(your\s+)?name\s*\]`)
	rePlaceholderTitle   = regexp.MustCompile(`(?i)\[\s*(your\s+)?(position|title)\s*\]`)
	rePlaceholderCompany = regexp.MustCompile(`(?i)\[\s*(your\s+)?company(\s+name)?\s*\]`)
	rePlaceholderTitleCo = regexp.MustCompile(`(?i)\[\s*(your\s+)?(title|position)\s*\/\s*(company|title)\s*\]`)
	rePlaceholderContact = regexp.MustCompile(`(?i)\[\s*(your\s+)?contact(\s+information)?(,\s*if\s+applicable)?\s*\]`)
	rePlaceholderGeneric = regexp.MustCompile(`(?i)\[\s*(phone\s+number|website|address)\s*\]`)
)

// ApplySignatureAndCleanPlaceholders replaces generic AI placeholders with configured profile values,
// or ensures a proper signature block is present.
func ApplySignatureAndCleanPlaceholders(body string, cfg *config.Config) string {
	if cfg == nil {
		return strings.TrimSpace(body)
	}

	res := body

	// Combined Title/Company placeholder
	if cfg.ReplyTitle != "" && cfg.CompanyName != "" {
		res = rePlaceholderTitleCo.ReplaceAllString(res, cfg.ReplyTitle+", "+cfg.CompanyName)
	} else if cfg.ReplyTitle != "" {
		res = rePlaceholderTitleCo.ReplaceAllString(res, cfg.ReplyTitle)
	} else if cfg.CompanyName != "" {
		res = rePlaceholderTitleCo.ReplaceAllString(res, cfg.CompanyName)
	} else {
		res = rePlaceholderTitleCo.ReplaceAllString(res, "")
	}

	if cfg.ReplyName != "" {
		res = rePlaceholderName.ReplaceAllString(res, cfg.ReplyName)
	} else {
		res = rePlaceholderName.ReplaceAllString(res, "")
	}

	if cfg.ReplyTitle != "" {
		res = rePlaceholderTitle.ReplaceAllString(res, cfg.ReplyTitle)
	} else {
		res = rePlaceholderTitle.ReplaceAllString(res, "")
	}

	if cfg.CompanyName != "" {
		res = rePlaceholderCompany.ReplaceAllString(res, cfg.CompanyName)
	} else {
		res = rePlaceholderCompany.ReplaceAllString(res, "")
	}

	if cfg.ReplyContact != "" {
		res = rePlaceholderContact.ReplaceAllString(res, cfg.ReplyContact)
	} else {
		res = rePlaceholderContact.ReplaceAllString(res, "")
	}

	res = rePlaceholderGeneric.ReplaceAllString(res, "")

	// Clean up empty/whitespace lines left by removed bracket placeholders
	lines := strings.Split(res, "\n")
	var cleanedLines []string
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		cleanedLines = append(cleanedLines, trimmed)
	}
	res = strings.TrimSpace(strings.Join(cleanedLines, "\n"))

	// If the body doesn't end with a sign-off, ensure the configured signature is appended
	lowerRes := strings.ToLower(res)
	if !strings.Contains(lowerRes, "best regards") && !strings.Contains(lowerRes, "sincerely") && !strings.Contains(lowerRes, "kind regards") {
		sig := cfg.FormattedSignature()
		if sig != "" {
			res = res + "\n\n" + sig
		}
	}

	return strings.TrimSpace(res)
}
