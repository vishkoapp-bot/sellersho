package repository

import (
	"context"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOPS02TapinAttemptClaimIsExclusive(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	order, _ := opsOrder(t, db, f, false)
	shipment := "ops02-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status) VALUES($1,$2,$3,'{}','09120000000','synthetic','processing')`, shipment, order, f.supplier)
	repo := NewTapinRepository(db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var claims atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if repo.MarkTapinRegisterAttempted(context.Background(), shipment) == nil {
				claims.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("registration guard admitted %d competing owners", claims.Load())
	}
}
func TestOPS02TapinAttemptLifecycleDoesNotReplayRegistration(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	order, _ := opsOrder(t, db, f, false)
	shipment := "ops02-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status) VALUES($1,$2,$3,'{}','09120000000','synthetic','processing')`, shipment, order, f.supplier)
	repo := NewTapinRepository(db)
	ctx := context.Background()
	if err := repo.MarkTapinRegisterAttempted(ctx, shipment); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClearTapinRegisterAttempted(ctx, shipment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkTapinRegisterAttempted(ctx, shipment); err != nil {
		t.Fatal("definite no-send did not rearm guard", err)
	}
	if err := repo.RecordTapinRegistration(ctx, shipment, "tipax", "synthetic", 0); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkTapinRegisterAttempted(ctx, shipment); err == nil {
		t.Fatal("recorded registration allowed another provider send")
	}
}
