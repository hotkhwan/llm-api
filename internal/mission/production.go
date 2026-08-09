package mission

import (
	"context"
	"fmt"
	"strings"
)

const ProductionSpecSchemaVersion = "kwanni.production/v1"

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
	caption, err := captioner.Generate(ctx, CaptionRequest{Product: request.Product, Shots: request.Shots})
	if err != nil {
		return PlanResult{}, err
	}
	roles := append([]RoleExecution(nil), defaultRoleExecutions...)
	for index := range roles {
		roles[index].Runtime = caption.Provider
	}
	return PlanResult{
		Caption: caption.Caption, CTA: caption.CTA, Hashtags: caption.Hashtags,
		ProductionSpec: deterministicProductionSpec(request.Product, request.Shots),
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
			result.Caption, result.CTA, result.Hashtags = verifiedProductCopy(request.Product)
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
	parts := []string{strings.TrimSpace(product.Name) + ": " + strings.TrimSpace(product.Description)}
	if facts := cleanStrings(product.Facts); len(facts) > 0 {
		parts = append(parts, "ข้อมูลที่ผู้ใช้ระบุ: "+strings.Join(facts, " · "))
	}
	if price := strings.TrimSpace(product.Price); price != "" {
		parts = append(parts, "ราคาที่ผู้ใช้ระบุ: "+price)
	}
	if promotion := strings.TrimSpace(product.Promotion); promotion != "" {
		parts = append(parts, "โปรโมชั่นที่ผู้ใช้ระบุ: "+promotion)
	}
	caption := strings.Join(parts, "\n")
	if runes := []rune(caption); len(runes) > 500 {
		caption = string(runes[:497]) + "..."
	}
	return caption, "ดูรายละเอียดสินค้าจากลิงก์ที่แนบไว้", []string{"#ลองแล้วบอกต่อ", "#Affiliate"}
}

func deterministicProductionSpec(product Product, guides []Shot) ProductionSpec {
	facts := make([]string, 0, len(product.Facts)+3)
	facts = append(facts, strings.TrimSpace(product.Description))
	facts = append(facts, cleanStrings(product.Facts)...)
	if strings.TrimSpace(product.Price) != "" {
		facts = append(facts, "ราคาที่ผู้ใช้ระบุ: "+strings.TrimSpace(product.Price))
	}
	if strings.TrimSpace(product.Promotion) != "" {
		facts = append(facts, "โปรโมชั่นที่ผู้ใช้ระบุ: "+strings.TrimSpace(product.Promotion))
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
		StoryBeats:     []string{"แสดงปัญหาหรือสภาพก่อนใช้", "สาธิตการใช้สินค้าจริง", "แสดงผลหลังใช้โดยไม่กล่าวอ้างเกินข้อมูล"},
		Continuity: ContinuityBible{
			Product:   ProductBible{Name: strings.TrimSpace(product.Name), VerifiedFacts: facts, RequiredDetails: []string{"สี รูปทรง และฉลากต้องตรงกับภาพสินค้าจริง"}, ForbiddenChanges: []string{"ห้ามเปลี่ยนโลโก้", "ห้ามสร้างคุณสมบัติ ราคา โปรโมชั่น หรือผลลัพธ์ที่ผู้ใช้ไม่ได้ระบุ"}},
			Character: CharacterBible{Description: "ผู้ใช้งานจริง", Constraints: []string{"รักษารูปลักษณ์เดิมตลอดทุกช็อต"}},
			Wardrobe:  WardrobeBible{ID: "look01", Description: "ชุดเดียวกันตลอดคลิป"},
			Makeup:    MakeupBible{ID: "makeup01", Description: "natural"},
			Location:  LocationBible{ID: "set01", Description: "สถานที่จริงของผู้ใช้"},
			Lighting:  LightingBible{ID: "light01", Style: "softNatural", ColorTemperatureKelvin: 5200},
			Camera:    CameraBible{Orientation: "vertical", AspectRatio: "9:16", Constraints: []string{"ไม่บิดรูปทรงสินค้า", "ให้ฉลากอ่านได้เมื่ออยู่ในเฟรม"}},
		},
		Shots: shots,
		ProviderPrompts: map[string]string{
			"veo":      "Render from productionSpec; preserve every continuity bible constraint and verified product fact.",
			"seedance": "Render from productionSpec; preserve every continuity bible constraint and verified product fact.",
		},
	}
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
