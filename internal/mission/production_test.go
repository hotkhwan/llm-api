package mission

import (
	"context"
	"testing"
)

type hallucinatingPlanner struct{}

func (hallucinatingPlanner) Plan(_ context.Context, request PlanRequest) (PlanResult, error) {
	return PlanResult{
		Caption:        "กล่องพลาสติกทนทานที่สุด รับน้ำหนักได้มาก",
		CTA:            "ซื้อเลย",
		Hashtags:       []string{"#ทนทาน"},
		ProductionSpec: deterministicProductionSpec(request.Product, request.Shots),
		Provider:       "local-qwen-role-planner",
	}, nil
}

func TestFallbackPlannerGroundsPublishedCopyInVerifiedProductFields(t *testing.T) {
	product := Product{Name: "กล่องสีขาว", Description: "กล่องพลาสติกสำหรับเก็บของชิ้นเล็ก", Facts: []string{"สีขาว", "วัสดุพลาสติก"}}
	shots := []Shot{{1, "ก่อน"}, {2, "ใช้"}, {3, "หลัง"}}
	result, err := (FallbackPlanner{Primary: hallucinatingPlanner{}}).Plan(context.Background(), PlanRequest{Product: product, Shots: shots})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "local-qwen-role-planner" {
		t.Fatalf("provider = %q", result.Provider)
	}
	if result.Caption != "กล่องสีขาว: กล่องพลาสติกสำหรับเก็บของชิ้นเล็ก\nข้อมูลที่ผู้ใช้ระบุ: สีขาว · วัสดุพลาสติก" {
		t.Fatalf("caption = %q", result.Caption)
	}
	if result.CTA != "ดูรายละเอียดสินค้าจากลิงก์ที่แนบไว้" || len(result.Hashtags) != 2 {
		t.Fatalf("copy = %#v", result)
	}
}

func TestFallbackPlannerGroundsCopyInRequestedLocale(t *testing.T) {
	product := Product{Name: "收纳盒", Description: "用于收纳小物", Facts: []string{"白色"}, Price: "¥29"}
	shots := localizedCaptureShots(LocaleChinese)
	result, err := (FallbackPlanner{Primary: hallucinatingPlanner{}}).Plan(context.Background(), PlanRequest{Product: product, Shots: shots, Locale: LocaleChinese})
	if err != nil {
		t.Fatal(err)
	}
	want := "收纳盒: 用于收纳小物\n用户提供的事实：白色\n用户提供的价格：¥29"
	if result.Caption != want || result.CTA != "请通过附带链接查看商品详情" {
		t.Fatalf("localized grounded copy = %#v", result)
	}
	if len(result.Hashtags) != 2 || result.Hashtags[0] != "#真实体验" {
		t.Fatalf("hashtags = %#v", result.Hashtags)
	}
	fallback, err := (CaptionBackedPlanner{Captions: FallbackCaptioner{}}).Plan(context.Background(), PlanRequest{Product: product, Shots: shots, Locale: LocaleChinese})
	if err != nil || fallback.ProductionSpec.StoryBeats[0] != "展示使用前的情况" {
		t.Fatalf("localized fallback spec=%#v err=%v", fallback.ProductionSpec.StoryBeats, err)
	}
}
