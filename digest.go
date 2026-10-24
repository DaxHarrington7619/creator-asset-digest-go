package digest

import (
	"fmt"
	"html"
	"strings"
)

type Subscriber struct {
	Email         string `json:"email"`
	Active        bool   `json:"active"`
	DigestEnabled bool   `json:"digest_enabled"`
}

type Asset struct {
	Title       string `json:"title"`
	DownloadURL string `json:"download_url"`
}

type Delivery struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type DigestRun struct {
	Edition     string       `json:"edition"`
	Subscribers []Subscriber `json:"subscribers"`
	Assets      []Asset      `json:"assets"`
}

func BuildDeliveries(run DigestRun) ([]Delivery, error) {
	if strings.TrimSpace(run.Edition) == "" {
		return nil, fmt.Errorf("edition is required")
	}
	if len(run.Assets) == 0 {
		return nil, fmt.Errorf("at least one asset is required")
	}

	var items strings.Builder
	items.WriteString("<ul>")
	for _, asset := range run.Assets {
		if strings.TrimSpace(asset.Title) == "" || strings.TrimSpace(asset.DownloadURL) == "" {
			return nil, fmt.Errorf("each asset needs a title and download_url")
		}
		fmt.Fprintf(&items, `<li><a href="%s">%s</a></li>`, html.EscapeString(asset.DownloadURL), html.EscapeString(asset.Title))
	}
	items.WriteString("</ul>")

	var deliveries []Delivery
	for _, subscriber := range run.Subscribers {
		if !subscriber.Active || !subscriber.DigestEnabled || strings.TrimSpace(subscriber.Email) == "" {
			continue
		}
		deliveries = append(deliveries, Delivery{
			To:      subscriber.Email,
			Subject: "Creator asset digest: " + run.Edition,
			HTML:    "<h1>" + html.EscapeString(run.Edition) + "</h1>" + items.String(),
		})
	}
	return deliveries, nil
}
