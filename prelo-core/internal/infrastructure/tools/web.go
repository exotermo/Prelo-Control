package tools

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// InspectWebsiteTool (Fase T, base of prospecting): fetches one page the way a visitor would —
// no JavaScript — and reports what matters for a business's online presence. The HTTP client
// must refuse internal addresses at dial time (webhook.NewHTTPClient), so a model can't use this
// to reach the services next to prelo-core.
type InspectWebsiteTool struct {
	client *http.Client
}

const maxPageBytes = 1 << 20

func NewInspectWebsiteTool(client *http.Client) *InspectWebsiteTool {
	safe := *client
	safe.Timeout = 12 * time.Second
	// Follow a few redirects (http→https, www) — every hop still goes through the guarded dialer.
	safe.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("redirecionamentos demais")
		}
		return nil
	}
	return &InspectWebsiteTool{client: &safe}
}

func (*InspectWebsiteTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("inspect_website",
		"Abre um site como um visitante (sem executar JavaScript) e informa: se responde, status, HTTPS, se é adaptado a celular, "+
			"título, descrição, tempo de resposta e links de WhatsApp/Instagram/Facebook. Só leitura.", domain.RiskLow)
	return def.WithSchema(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`,
		"Nenhum: só visita a página pública indicada.")
}

var (
	titleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaRe     = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	nameAttrRe = regexp.MustCompile(`(?is)\b(?:name|property)\s*=\s*["']([^"']+)["']`)
	contentRe  = regexp.MustCompile(`(?is)\bcontent\s*=\s*["']([^"']*)["']`)
	hrefRe     = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']+)["']`)
)

func (t *InspectWebsiteTool) Execute(ctx context.Context, _ domain.Execution, argsJSON string) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	raw := strings.TrimSpace(args.URL)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil {
		return "", &domain.ValidationError{Message: "url inválida: use http(s)://dominio"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", &domain.ValidationError{Message: "url inválida"}
	}
	req.Header.Set("User-Agent", "Prelo-Inspector/1 (+verificação de presença digital)")
	req.Header.Set("Accept", "text/html,*/*;q=0.5")
	started := time.Now()
	resp, err := t.client.Do(req)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return jsonOutput(map[string]any{"url": target.String(), "reachable": false, "error": friendlyFetchError(err), "elapsedMs": elapsed})
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	page := string(body)

	report := map[string]any{
		"url": target.String(), "finalUrl": resp.Request.URL.String(), "reachable": true, "status": resp.StatusCode,
		"https": resp.Request.URL.Scheme == "https", "elapsedMs": elapsed,
		"contentType": resp.Header.Get("Content-Type"),
	}
	if m := titleRe.FindStringSubmatch(page); m != nil {
		report["title"] = truncate(strings.TrimSpace(html.UnescapeString(m[1])), 200)
	}
	mobile := false
	for _, tag := range metaRe.FindAllString(page, 60) {
		name := nameAttrRe.FindStringSubmatch(tag)
		content := contentRe.FindStringSubmatch(tag)
		if name == nil || content == nil {
			continue
		}
		switch strings.ToLower(name[1]) {
		case "viewport":
			mobile = strings.Contains(strings.ToLower(content[1]), "width=device-width")
		case "description", "og:description":
			if _, set := report["description"]; !set {
				report["description"] = truncate(strings.TrimSpace(html.UnescapeString(content[1])), 300)
			}
		}
	}
	report["mobileFriendly"] = mobile
	social := map[string]string{}
	for _, m := range hrefRe.FindAllStringSubmatch(page, 400) {
		link := strings.ToLower(m[1])
		switch {
		case strings.Contains(link, "wa.me/") || strings.Contains(link, "api.whatsapp.com"):
			social["whatsapp"] = m[1]
		case strings.Contains(link, "instagram.com/"):
			social["instagram"] = m[1]
		case strings.Contains(link, "facebook.com/"):
			social["facebook"] = m[1]
		}
	}
	report["social"] = social
	return jsonOutput(report)
}

func friendlyFetchError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "private") || strings.Contains(msg, "internal"):
		return "endereço interno não é permitido"
	case strings.Contains(msg, "no such host"):
		return "domínio não existe ou não resolve"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "o site não respondeu a tempo"
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "certificado HTTPS inválido"
	case strings.Contains(msg, "redirecionamentos"):
		return "redirecionamentos demais"
	default:
		return "não foi possível abrir o site"
	}
}
