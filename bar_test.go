package get

import (
	"io"
	"strings"
	"testing"

	"github.com/cheggaaa/pb/v3"
	"github.com/stretchr/testify/assert"
)

// TestProgressBarTemplateRender checks the download progress bar template:
// a borderless ■/□ bar with blue percent and yellow speed.
// Color funcs (yellow/blue) are pb's builtin template funcs; a wrong template
// name or syntax would fail the template parse, reported by bar.Err().
func TestProgressBarTemplateRender(t *testing.T) {
	bar := pb.ProgressBarTemplate(progressBarTemplate).New(100).SetWriter(io.Discard)
	assert.NoError(t, bar.Err(), "template must parse without error")

	bar.SetCurrent(75)
	out := bar.String()

	assert.Contains(t, out, "■", "bar must render filled cells")
	assert.Contains(t, out, "□", "bar must render empty cells")
	assert.False(t, strings.Contains(out, "["), "bar must be borderless")
	assert.Contains(t, out, "75%", "percent must be rendered")
	assert.Contains(t, out, "?/s", "speed placeholder must be rendered before first speed sample")

	t.Logf("rendered: %q", out)
}
