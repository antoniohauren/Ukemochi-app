package db

import (
	"context"
	"testing"
)

func TestOpeningCash(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if got, err := d.OpeningCash(ctx, "2026-11-20"); err != nil || got != 0 {
		t.Fatalf("unset day = %d, %v", got, err)
	}
	for _, v := range []int64{20000, 15050} {
		if err := d.SetOpeningCash(ctx, "2026-11-20", v); err != nil {
			t.Fatal(err)
		}
		if got, err := d.OpeningCash(ctx, "2026-11-20"); err != nil || got != v {
			t.Errorf("after set %d: got %d, %v", v, got, err)
		}
	}
	if got, _ := d.OpeningCash(ctx, "2026-11-21"); got != 0 {
		t.Errorf("other day = %d", got)
	}
	if err := d.SetOpeningCash(ctx, "2026-11-20", -1); err == nil {
		t.Error("negative opening cash accepted")
	}
}
