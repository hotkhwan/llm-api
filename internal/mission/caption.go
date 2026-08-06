package mission

import (
	"context"
	"fmt"
	"strings"
)

// FallbackCaptioner permits a local-LLM adapter to be added without making the
// first mission depend on model availability.
type FallbackCaptioner struct{ Primary CaptionGenerator }

func (g FallbackCaptioner) Generate(ctx context.Context, request CaptionRequest) (CaptionResult, error) {
	if g.Primary != nil {
		if result, err := g.Primary.Generate(ctx, request); err == nil && safeGeneratedContent(result) {
			return result, nil
		}
	}
	name := strings.TrimSpace(request.Product.Name)
	return CaptionResult{
		Caption:  fmt.Sprintf("ลอง %s แบบง่าย ๆ ตาม 3 ขั้นตอนในคลิป", name),
		CTA:      "ดูรายละเอียดสินค้าจากลิงก์ที่แนบไว้",
		Hashtags: []string{"#ลองแล้วบอกต่อ", "#Affiliate"},
		Provider: "deterministic-fallback",
	}, nil
}

func safeGeneratedContent(result CaptionResult) bool {
	combined := strings.ToLower(strings.Join(append([]string{result.Caption, result.CTA}, result.Hashtags...), " "))
	if strings.TrimSpace(result.Caption) == "" || len([]rune(result.Caption)) > 500 || len(result.Hashtags) > 12 {
		return false
	}
	for _, prohibited := range []string{"รับประกันรายได้", "รายได้แน่นอน", "รวยเร็ว", "guaranteed income", "get rich quick"} {
		if strings.Contains(combined, prohibited) {
			return false
		}
	}
	return true
}
