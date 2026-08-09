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
