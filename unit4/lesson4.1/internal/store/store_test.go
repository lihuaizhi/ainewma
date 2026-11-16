package store

import "testing"

func TestGetOrder(t *testing.T) {
	o, err := GetOrder("O-1001")
	if err != nil {
		t.Fatalf("O-1001: %v", err)
	}
	if o.SKU != "无线蓝牙耳机 Air3" || o.Status != "待发货" {
		t.Fatalf("unexpected order: %+v", o)
	}
	if _, err := GetOrder("O-9999"); err == nil {
		t.Fatal("want error for unknown order")
	}
}

func TestSubmitRefundGuardRails(t *testing.T) {
	// 空原因拒绝
	if _, err := SubmitRefund("O-1002", ""); err == nil {
		t.Fatal("want error for empty reason")
	}
	// 未知订单拒绝
	if _, err := SubmitRefund("O-9999", "不想要了"); err == nil {
		t.Fatal("want error for unknown order")
	}
	// 待付款拒绝
	if _, err := SubmitRefund("O-1005", "不想要了"); err == nil {
		t.Fatal("want error for unpaid order")
	}
	// 正常退款
	id, err := SubmitRefund("O-1001", "更喜欢另一款")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if id == "" {
		t.Fatal("empty refund id")
	}
	// 重复退款拒绝
	if _, err := SubmitRefund("O-1001", "再退一次"); err == nil {
		t.Fatal("want error for double refund")
	}
}

func TestPolicyLookup(t *testing.T) {
	all := GetPolicy("")
	if len(all) == 0 {
		t.Fatal("want default policies")
	}
	hit := GetPolicy("七天无理由")
	if len(hit) == 0 || hit[0].Topic != "七天无理由" {
		t.Fatalf("want 七天无理由 policy, got %+v", hit)
	}
}

func TestSearchProductsBudget(t *testing.T) {
	got := SearchProducts("数码", 300, 10)
	if len(got) == 0 {
		t.Fatal("want hits for 数码")
	}
	for _, p := range got {
		if p.Price > 300 {
			t.Fatalf("%s price %.0f exceeds budget", p.ID, p.Price)
		}
	}
}
