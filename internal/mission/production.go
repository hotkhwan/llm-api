package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const ProductionSpecSchemaVersion = "kwanni.production/v1"

const (
	VeoAdapterVersion      = "kwanni.veo/v1"
	SeedanceAdapterVersion = "kwanni.seedance/v1"
)

var defaultRoleExecutions = []RoleExecution{
	{Role: RoleCreativeDirector, Runtime: "shared-qwen"},
	{Role: RoleStoryDirector, Runtime: "shared-qwen"},
	{Role: RoleBrandGuard, Runtime: "shared-qwen"},
	{Role: RoleProductionPlanner, Runtime: "shared-qwen"},
	{Role: RolePromptCompiler, Runtime: "shared-qwen"},
}

// CaptionBackedPlanner preserves the graceful deterministic/LLM caption path
// while producing a complete provider-neutral production specification.
type CaptionBackedPlanner struct{ Captions CaptionGenerator }

func (p CaptionBackedPlanner) Plan(ctx context.Context, request PlanRequest) (PlanResult, error) {
	captioner := p.Captions
	if captioner == nil {
		captioner = FallbackCaptioner{}
	}
	caption, err := captioner.Generate(ctx, CaptionRequest{Product: request.Product, Shots: request.Shots, Locale: request.Locale})
	if err != nil {
		return PlanResult{}, err
	}
	roles := append([]RoleExecution(nil), defaultRoleExecutions...)
	for index := range roles {
		roles[index].Runtime = caption.Provider
	}
	return PlanResult{
		Caption: caption.Caption, CTA: caption.CTA, Hashtags: caption.Hashtags,
		ProductionSpec: deterministicProductionSpecForLocale(request.Product, request.Shots, request.Locale),
		Roles:          roles,
		Provider:       caption.Provider, Units: caption.Units, CostMicros: caption.CostMicros,
	}, nil
}

type FallbackPlanner struct {
	Primary  ProductionPlanner
	Fallback ProductionPlanner
}

func (p FallbackPlanner) Plan(ctx context.Context, request PlanRequest) (PlanResult, error) {
	if p.Primary != nil {
		if result, err := p.Primary.Plan(ctx, request); err == nil && safeGeneratedContent(CaptionResult{Caption: result.Caption, CTA: result.CTA, Hashtags: result.Hashtags}) && validateProductionSpec(result.ProductionSpec) == nil {
			// Brand Guard is deterministic at the publication boundary. Qwen owns
			// the creative production plan, but user-facing product claims are
			// rebuilt only from operator-verified fields so plausible adjectives
			// (for example "durable") cannot silently become product facts.
			result.Caption, result.CTA, result.Hashtags = verifiedProductCopyForLocale(request.Product, request.Locale)
			return result, nil
		}
	}
	fallback := p.Fallback
	if fallback == nil {
		fallback = CaptionBackedPlanner{Captions: FallbackCaptioner{}}
	}
	return fallback.Plan(ctx, request)
}

func verifiedProductCopy(product Product) (string, string, []string) {
	return verifiedProductCopyForLocale(product, LocaleThai)
}

func verifiedProductCopyForLocale(product Product, locale Locale) (string, string, []string) {
	locale, _ = normalizeLocale(locale)
	parts := []string{strings.TrimSpace(product.Name) + ": " + strings.TrimSpace(product.Description)}
	if facts := cleanStrings(product.Facts); len(facts) > 0 {
		parts = append(parts, localized(locale, "ข้อมูลที่ผู้ใช้ระบุ: ", "User-provided facts: ", "用户提供的事实：")+strings.Join(facts, " · "))
	}
	if price := strings.TrimSpace(product.Price); price != "" {
		parts = append(parts, localized(locale, "ราคาที่ผู้ใช้ระบุ: ", "User-provided price: ", "用户提供的价格：")+price)
	}
	if promotion := strings.TrimSpace(product.Promotion); promotion != "" {
		parts = append(parts, localized(locale, "โปรโมชั่นที่ผู้ใช้ระบุ: ", "User-provided promotion: ", "用户提供的促销信息：")+promotion)
	}
	caption := strings.Join(parts, "\n")
	if runes := []rune(caption); len(runes) > 500 {
		caption = string(runes[:497]) + "..."
	}
	switch locale {
	case LocaleEnglish:
		return caption, "See the attached link for product details", []string{"#TriedAndShared", "#Affiliate"}
	case LocaleChinese:
		return caption, "请通过附带链接查看商品详情", []string{"#真实体验", "#好物分享"}
	default:
		return caption, "ดูรายละเอียดสินค้าจากลิงก์ที่แนบไว้", []string{"#ลองแล้วบอกต่อ", "#Affiliate"}
	}
}

func deterministicProductionSpec(product Product, guides []Shot) ProductionSpec {
	return deterministicProductionSpecForLocale(product, guides, LocaleThai)
}

func deterministicProductionSpecForLocale(product Product, guides []Shot, locale Locale) ProductionSpec {
	locale, _ = normalizeLocale(locale)
	facts := make([]string, 0, len(product.Facts)+3)
	facts = append(facts, strings.TrimSpace(product.Description))
	facts = append(facts, cleanStrings(product.Facts)...)
	if strings.TrimSpace(product.Price) != "" {
		facts = append(facts, localized(locale, "ราคาที่ผู้ใช้ระบุ: ", "User-provided price: ", "用户提供的价格：")+strings.TrimSpace(product.Price))
	}
	if strings.TrimSpace(product.Promotion) != "" {
		facts = append(facts, localized(locale, "โปรโมชั่นที่ผู้ใช้ระบุ: ", "User-provided promotion: ", "用户提供的促销信息：")+strings.TrimSpace(product.Promotion))
	}
	shots := make([]ProductionShot, 0, len(guides))
	for index, guide := range guides {
		movement := "static"
		shotSize := "medium"
		if index == 0 {
			shotSize = "wide"
		} else if index == len(guides)-1 {
			movement = "slowPushIn"
			shotSize = "closeUp"
		}
		shots = append(shots, ProductionShot{
			ShotID: fmt.Sprintf("shot%02d", guide.Number), CaptureShot: guide.Number,
			DurationSeconds: 3, ShotSize: shotSize, CameraAngle: "eyeLevel", LensMM: 50,
			CameraMovement: movement, SubjectAction: guide.Instruction,
			Composition: "centerWeighted",
			Lighting:    ShotLighting{Style: "softNatural", KeyDirection: "cameraLeft", ColorTemperatureKelvin: 5200},
			Continuity:  ShotContinuity{ProductOrientation: "labelFacingCamera", WardrobeID: "look01", LocationID: "set01"},
		})
	}
	return ProductionSpec{
		SchemaVersion: ProductionSpecSchemaVersion, ProjectType: "affiliateShort",
		DurationSeconds: len(shots) * 3, Platform: "tiktok", AspectRatio: "9:16",
		CreativeIntent: "truthfulProductDemo",
		StoryBeats: localizedList(locale,
			[]string{"แสดงปัญหาหรือสภาพก่อนใช้", "สาธิตการใช้สินค้าจริง", "แสดงผลหลังใช้โดยไม่กล่าวอ้างเกินข้อมูล"},
			[]string{"Show the situation before use", "Demonstrate the real product in use", "Show the result without exceeding verified facts"},
			[]string{"展示使用前的情况", "演示真实商品的使用过程", "只依据已核实信息展示使用结果"}),
		Continuity: ContinuityBible{
			Product: ProductBible{Name: strings.TrimSpace(product.Name), VerifiedFacts: facts,
				RequiredDetails:  localizedList(locale, []string{"สี รูปทรง และฉลากต้องตรงกับภาพสินค้าจริง"}, []string{"Color, shape, and label must match the real product reference"}, []string{"颜色、形状和标签必须与真实商品参考图一致"}),
				ForbiddenChanges: localizedList(locale, []string{"ห้ามเปลี่ยนโลโก้", "ห้ามสร้างคุณสมบัติ ราคา โปรโมชั่น หรือผลลัพธ์ที่ผู้ใช้ไม่ได้ระบุ"}, []string{"Do not alter the logo", "Do not invent features, prices, promotions, or results"}, []string{"不得更改品牌标志", "不得虚构功能、价格、促销或效果"})},
			Character: CharacterBible{Description: localized(locale, "ผู้ใช้งานจริง", "real product user", "真实商品使用者"), Constraints: localizedList(locale, []string{"รักษารูปลักษณ์เดิมตลอดทุกช็อต"}, []string{"Keep the same appearance across every shot"}, []string{"所有镜头中的人物外观保持一致"})},
			Wardrobe:  WardrobeBible{ID: "look01", Description: localized(locale, "ชุดเดียวกันตลอดคลิป", "same outfit throughout the video", "整段视频保持同一套服装")},
			Makeup:    MakeupBible{ID: "makeup01", Description: "natural"},
			Location:  LocationBible{ID: "set01", Description: localized(locale, "สถานที่จริงของผู้ใช้", "the user's real location", "用户的真实场景")},
			Lighting:  LightingBible{ID: "light01", Style: "softNatural", ColorTemperatureKelvin: 5200},
			Camera: CameraBible{Orientation: "vertical", AspectRatio: "9:16", Constraints: localizedList(locale,
				[]string{"ไม่บิดรูปทรงสินค้า", "ให้ฉลากอ่านได้เมื่ออยู่ในเฟรม"},
				[]string{"Do not distort the product shape", "Keep the label readable when it is in frame"},
				[]string{"不得扭曲商品形状", "标签入镜时必须清晰可读"})},
		},
		Shots: shots,
		ProviderPrompts: map[string]string{
			"veo":      "Render from productionSpec; preserve every continuity bible constraint and verified product fact.",
			"seedance": "Render from productionSpec; preserve every continuity bible constraint and verified product fact.",
		},
	}
}

func normalizeLocale(locale Locale) (Locale, error) {
	switch Locale(strings.ToLower(strings.TrimSpace(string(locale)))) {
	case "", LocaleThai:
		return LocaleThai, nil
	case LocaleEnglish:
		return LocaleEnglish, nil
	case LocaleChinese:
		return LocaleChinese, nil
	default:
		return "", fmt.Errorf("locale must be th, en, or zh")
	}
}

func localized(locale Locale, thai, english, chinese string) string {
	switch locale {
	case LocaleEnglish:
		return english
	case LocaleChinese:
		return chinese
	default:
		return thai
	}
}

func localizedList(locale Locale, thai, english, chinese []string) []string {
	switch locale {
	case LocaleEnglish:
		return english
	case LocaleChinese:
		return chinese
	default:
		return thai
	}
}

func localizedCaptureShots(locale Locale) []Shot {
	switch locale {
	case LocaleEnglish:
		return []Shot{{1, "Capture a photo or video before using the product"}, {2, "Capture the product while it is being used"}, {3, "Capture the result after using the product"}}
	case LocaleChinese:
		return []Shot{{1, "拍摄使用商品前的照片或视频"}, {2, "拍摄正在使用商品的过程"}, {3, "拍摄使用商品后的结果"}}
	default:
		return []Shot{{1, "ถ่ายภาพหรือคลิปก่อนใช้สินค้า"}, {2, "ถ่ายตอนกำลังใช้สินค้า"}, {3, "ถ่ายผลลัพธ์หลังใช้สินค้า"}}
	}
}

func compileRenderPrompts(spec ProductionSpec, locale Locale) (RenderPrompts, error) {
	locale, err := normalizeLocale(locale)
	if err != nil {
		return RenderPrompts{}, err
	}
	if err := validateProductionSpec(spec); err != nil {
		return RenderPrompts{}, err
	}
	if len(spec.Continuity.Product.ReferenceKeys) != 1 {
		return RenderPrompts{}, fmt.Errorf("render adapters require exactly one product reference")
	}
	canonical := spec
	canonical.ProviderPrompts = nil
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return RenderPrompts{}, fmt.Errorf("encode canonical production spec: %w", err)
	}
	veoInstruction := localized(locale,
		"สร้างวิดีโอแนวตั้งจากสเปก JSON นี้ ยึดภาพสินค้าอ้างอิงเป็นความจริงด้านภาพ รักษาสินค้า ตัวละคร เสื้อผ้า สถานที่ แสง และกล้องให้ต่อเนื่องครบ 3 ช็อต ห้ามเพิ่มคำกล่าวอ้างที่ไม่มีใน verifiedFacts",
		"Create a vertical video from this JSON spec. Treat the product reference as visual truth. Preserve product, character, wardrobe, location, lighting, and camera continuity across all three shots. Do not add claims absent from verifiedFacts.",
		"根据此 JSON 规格生成竖屏视频。以商品参考图为视觉事实依据，确保三个镜头中的商品、人物、服装、场景、灯光和摄影连续一致。不得添加 verifiedFacts 中没有的宣传内容。")
	seedanceInstruction := localized(locale,
		"เรนเดอร์วิดีโอ 9:16 ตามลำดับ 3 ช็อตในสเปก JSON นี้ ใช้ภาพสินค้าอ้างอิงล็อกสี รูปทรง ฉลาก และโลโก้ ทำตามเวลา การเคลื่อนกล้อง และ continuity ทุกข้อ ห้ามสร้างคุณสมบัติหรือผลลัพธ์ใหม่",
		"Render a 9:16 video following the three-shot sequence in this JSON spec. Use the product reference to lock color, shape, label, and logo. Follow timing, camera movement, and every continuity constraint. Do not invent features or outcomes.",
		"按照此 JSON 规格中的三个镜头顺序渲染 9:16 视频。使用商品参考图锁定颜色、形状、标签和品牌标志，并遵循时长、镜头运动及全部连续性约束。不得虚构功能或效果。")
	heading := localized(locale, "Canonical Production Spec JSON (ข้อมูลหลัก):", "Canonical Production Spec JSON:", "标准制作规格 JSON：")
	return RenderPrompts{
		Veo:      RenderPrompt{Provider: "veo", AdapterVersion: VeoAdapterVersion, Prompt: veoInstruction + "\n" + heading + "\n" + string(encoded)},
		Seedance: RenderPrompt{Provider: "seedance", AdapterVersion: SeedanceAdapterVersion, Prompt: seedanceInstruction + "\n" + heading + "\n" + string(encoded)},
	}, nil
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validateProductionSpec(spec ProductionSpec) error {
	if spec.SchemaVersion != ProductionSpecSchemaVersion || spec.ProjectType != "affiliateShort" || spec.AspectRatio != "9:16" {
		return fmt.Errorf("invalid canonical production spec identity")
	}
	if spec.DurationSeconds < 6 || spec.DurationSeconds > 25 || len(spec.Shots) != 3 {
		return fmt.Errorf("production spec must contain three shots totaling 6-25 seconds")
	}
	total := 0
	seen := map[string]bool{}
	for _, shot := range spec.Shots {
		if shot.ShotID == "" || seen[shot.ShotID] || shot.CaptureShot < 1 || shot.CaptureShot > 3 || shot.DurationSeconds < 1 {
			return fmt.Errorf("invalid production shot")
		}
		seen[shot.ShotID] = true
		total += shot.DurationSeconds
	}
	if total != spec.DurationSeconds || strings.TrimSpace(spec.Continuity.Product.Name) == "" {
		return fmt.Errorf("production spec duration or product bible is inconsistent")
	}
	return nil
}
