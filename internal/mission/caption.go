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
	locale, _ := normalizeLocale(request.Locale)
	caption := fmt.Sprintf("ลอง %s แบบง่าย ๆ ตาม 3 ขั้นตอนในคลิป", name)
	cta := "ดูรายละเอียดสินค้าจากลิงก์ที่แนบไว้"
	hashtags := []string{"#ลองแล้วบอกต่อ", "#Affiliate"}
	if locale == LocaleEnglish {
		caption = fmt.Sprintf("Try %s in three simple steps shown in the video", name)
		cta = "See the attached link for product details"
		hashtags = []string{"#TriedAndShared", "#Affiliate"}
	} else if locale == LocaleChinese {
		caption = fmt.Sprintf("通过视频中的三个简单步骤体验%s", name)
		cta = "请通过附带链接查看商品详情"
		hashtags = []string{"#真实体验", "#好物分享"}
	}
	return CaptionResult{
		Caption: caption, CTA: cta, Hashtags: hashtags,
		Provider: "deterministic-fallback",
	}, nil
}

func safeGeneratedContent(result CaptionResult) bool {
	combined := strings.ToLower(strings.Join(append([]string{result.Caption, result.CTA}, result.Hashtags...), " "))
	if strings.TrimSpace(result.Caption) == "" || len([]rune(result.Caption)) > 500 || len(result.Hashtags) > 12 {
		return false
	}
	for _, prohibited := range []string{"รับประกันรายได้", "รับประกันยอดขาย", "รายได้แน่นอน", "รายได้ทุกวัน", "รวยเร็ว", "ทำเงินอัตโนมัติ", "ai ทำเงินแทนคุณ", "guaranteed income", "guaranteed sales", "get rich quick"} {
		if strings.Contains(combined, prohibited) {
			return false
		}
	}
	return true
}
