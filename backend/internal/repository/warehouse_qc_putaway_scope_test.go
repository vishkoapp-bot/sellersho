package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestQUAL01WarehouseQCAndPutawayCityScope(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	wh := NewWarehouseRepository(db)
	local, remote := "w-"+uuid.NewString(), "w-"+uuid.NewString()
	inbound := inv09ReceiveLegacy(t, db, f, local, 3)
	otherInbound := inv09ReceiveLegacy(t, db, f, remote, 3)
	opsExec(t, db, `UPDATE warehouses SET city_name='other' WHERE id=$1`, remote)
	opsExec(t, db, `UPDATE users SET warehouse_city='c',warehouse_city_confirmed=false WHERE id=$1`, f.user)
	ctx := WithWarehouseStaffID(context.Background(), f.user)
	qcContains := func(wantLocal, wantRemote bool) {
		t.Helper()
		q, err := wh.GetQCQueue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var sawLocal, sawRemote bool
		for _, item := range q {
			sawLocal = sawLocal || item.InboundID == inbound
			sawRemote = sawRemote || item.InboundID == otherInbound
		}
		if sawLocal != wantLocal || sawRemote != wantRemote {
			t.Fatalf("QC queue local=%t remote=%t", sawLocal, sawRemote)
		}
	}
	denied := func(err error) {
		t.Helper()
		if !errors.Is(err, ErrInboundProvenance) {
			t.Fatalf("expected scope rejection, got %v", err)
		}
	}
	qcContains(false, false)
	denied(wh.RecordQCPass(ctx, f.product, inbound, f.user, 1))
	opsExec(t, db, `UPDATE users SET warehouse_city_confirmed=true,is_active=false WHERE id=$1`, f.user)
	qcContains(false, false)
	denied(wh.RecordQCPass(ctx, f.product, inbound, f.user, 1))
	opsExec(t, db, `UPDATE users SET is_active=true WHERE id=$1`, f.user)
	qcContains(true, false)
	denied(wh.RecordQCPass(ctx, f.product, otherInbound, f.user, 1))
	_, err := wh.CreateQCDiscrepancy(ctx, map[string]interface{}{"inboundId": otherInbound, "supplierId": f.supplier, "productId": f.product})
	denied(err)
	if err = wh.RecordQCPass(ctx, f.product, inbound, f.user, 1); err != nil {
		t.Fatal(err)
	}
	if err = wh.RecordQCPass(context.Background(), f.product, otherInbound, f.user, 1); err != nil {
		t.Fatal(err)
	}
	var task, remoteTask string
	for _, pair := range []struct {
		inbound string
		task    *string
	}{{inbound, &task}, {otherInbound, &remoteTask}} {
		if err := db.QueryRow(`SELECT task_id FROM inbound_putaway_allocations WHERE shipment_id=$1`, pair.inbound).Scan(pair.task); err != nil {
			t.Fatal(err)
		}
	}
	q, err := wh.GetPutawayQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawLocal bool
	for _, item := range q {
		if item.ID == remoteTask {
			t.Fatal("cross-city task in queue")
		}
		sawLocal = sawLocal || item.ID == task
	}
	if !sawLocal {
		t.Fatal("local task missing from queue")
	}
	localBin, remoteBin := "BC-"+uuid.NewString()[:12], "BC-"+uuid.NewString()[:12]
	for _, pair := range [][2]string{{localBin, local}, {remoteBin, remote}} {
		// The same display name does not make these bins interchangeable.
		opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,warehouse_id,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status) VALUES($1,'test','1','1',0,$2,$3,100000,100000,'verified')`, pair[0], pair[1], local)
	}
	denied(wh.AssignPutawayTask(ctx, remoteTask, remoteBin, f.user))
	denied(wh.AssignPutawayTask(ctx, task, remoteBin, f.user))
	opsExec(t, db, `UPDATE warehouses SET city_name='c' WHERE id=$1`, remote)
	denied(wh.AssignPutawayTask(ctx, task, remoteBin, f.user))
	opsExec(t, db, `UPDATE warehouses SET city_name='other' WHERE id=$1`, remote)
	assigned, err := wh.AssignPutaway(ctx, f.product, localBin, 1, f.user)
	if err != nil || assigned != task {
		t.Fatalf("scoped product assignment task=%q err=%v", assigned, err)
	}
	denied(wh.ConfirmPutaway(ctx, remoteTask, f.user))
	if err = wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE users SET is_active=false WHERE id=$1`, f.user)
	denied(wh.ConfirmPutaway(ctx, task, f.user))
	opsExec(t, db, `UPDATE users SET warehouse_city_confirmed=false,is_active=true WHERE id=$1`, f.user)
	denied(wh.ConfirmPutaway(ctx, task, f.user))
}
