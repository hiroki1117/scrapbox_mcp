package tools

import (
	"context"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

const (
	defaultSmartContextMaxChars = 100000
	maxSmartContextMaxChars     = 500000
)

// pageStartPattern matches the start of a <Page> block (but not <PageList>)
var pageStartPattern = regexp.MustCompile(`<Page[\s>]`)

type GetSmartContextTool struct {
	client *scrapbox.Client
}

func NewGetSmartContextTool(client *scrapbox.Client) *GetSmartContextTool {
	return &GetSmartContextTool{client: client}
}

func (t *GetSmartContextTool) Name() string {
	return "get_smart_context"
}

func (t *GetSmartContextTool) Description() string {
	return "Exports a Scrapbox page together with its related pages (linked, back-linked, and optionally 2-hop linked pages) as a single AI-ready text. " +
		"Same as \"Export for AI\" (Smart Context) in the Scrapbox page menu. " +
		"Use this instead of calling get_page repeatedly when you need the surrounding context of a topic. " +
		"Start with hops=1; use hops=2 only when more context is needed, because the output can exceed 1 MB. " +
		"Long output is truncated at page boundaries; call again with the offset shown in the truncation notice to continue."
}

func (t *GetSmartContextTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the page to start from",
			},
			"hops": map[string]interface{}{
				"type":        "number",
				"enum":        []int{1, 2},
				"description": "1: include directly linked/back-linked pages. 2: also include 2-hop linked pages (much larger). Default: 1",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
			"max_chars": map[string]interface{}{
				"type":        "number",
				"description": fmt.Sprintf("Maximum number of characters to return (default: %d, max: %d)", defaultSmartContextMaxChars, maxSmartContextMaxChars),
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Character offset to start from, used to continue a truncated result (default: 0)",
			},
		},
		"required": []string{"title"},
	}
}

func (t *GetSmartContextTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	title, ok := arguments["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a string")
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	hops := 1
	if hopsArg, ok := arguments["hops"].(float64); ok {
		hops = int(hopsArg)
	}
	if hops != 1 && hops != 2 {
		return nil, fmt.Errorf("hops must be 1 or 2")
	}

	maxChars := defaultSmartContextMaxChars
	if maxCharsArg, ok := arguments["max_chars"].(float64); ok {
		maxChars = int(maxCharsArg)
	}
	if maxChars <= 0 || maxChars > maxSmartContextMaxChars {
		return nil, fmt.Errorf("max_chars must be between 1 and %d", maxSmartContextMaxChars)
	}

	offset := 0
	if offsetArg, ok := arguments["offset"].(float64); ok {
		offset = int(offsetArg)
	}
	if offset < 0 {
		return nil, fmt.Errorf("offset must be 0 or greater")
	}

	text, err := t.client.RESTClient.ExportSmartContext(project, title, hops)
	if err != nil {
		return nil, err
	}

	return truncateSmartContext(text, offset, maxChars)
}

// truncateSmartContext returns up to maxChars characters of text starting at offset.
// When the text is truncated, it cuts at the last <Page> boundary within the limit
// (falling back to a plain character cut) and appends a notice with the next offset.
func truncateSmartContext(text string, offset, maxChars int) (string, error) {
	runes := []rune(text)
	total := len(runes)

	if offset > total || (offset == total && total > 0) {
		return "", fmt.Errorf("offset %d is out of range (total %d chars)", offset, total)
	}

	limit := offset + maxChars
	if limit >= total {
		return string(runes[offset:]), nil
	}

	// Cut at the last page start in (offset, limit] so that pages are not split.
	// If no page boundary is found, fall back to a plain cut at limit.
	end := limit
	pos, prevByte := 0, 0
	for _, loc := range pageStartPattern.FindAllStringIndex(text, -1) {
		pos += utf8.RuneCountInString(text[prevByte:loc[0]])
		prevByte = loc[0]
		if pos > limit {
			break
		}
		if pos > offset {
			end = pos
		}
	}

	return fmt.Sprintf("%s\n\n[truncated: returned chars %d-%d of %d. Call again with offset=%d to continue, or use hops=1.]",
		string(runes[offset:end]), offset, end, total, end), nil
}
