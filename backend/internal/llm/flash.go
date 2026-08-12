package llm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/genai"
)

// flashRe matches base text Gemini Flash model ids (gemini-2.5-flash,
// gemini-3.6-flash), excluding variants like -lite, -image, -preview, -live.
var flashRe = regexp.MustCompile(`^gemini-(\d+)(?:\.(\d+))?-flash$`)

// LatestFlashEndpoint lists the models visible to the Vertex client and
// returns the newest base Gemini Flash id (e.g. "gemini-3.6-flash"). Used to
// keep the BigQuery tagging remote model on the current Flash generation
// without a config bump (there is no floating "latest" alias for remote models).
func LatestFlashEndpoint(ctx context.Context, client *genai.Client) (string, error) {
	var ids []string
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			return "", fmt.Errorf("list models: %w", err)
		}
		id := m.Name
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		ids = append(ids, id)
	}
	latest := PickLatestFlash(ids)
	if latest == "" {
		return "", fmt.Errorf("no base gemini flash model among %d listed models", len(ids))
	}
	return latest, nil
}

// PickLatestFlash returns the highest-versioned base Flash id from ids
// (comparing major.minor numerically), or "" when none match.
func PickLatestFlash(ids []string) string {
	var best string
	var bestMajor, bestMinor int
	for _, id := range ids {
		m := flashRe.FindStringSubmatch(id)
		if m == nil {
			continue
		}
		major, _ := strconv.Atoi(m[1])
		minor := 0
		if m[2] != "" {
			minor, _ = strconv.Atoi(m[2])
		}
		if best == "" || major > bestMajor || (major == bestMajor && minor > bestMinor) {
			best, bestMajor, bestMinor = id, major, minor
		}
	}
	return best
}
