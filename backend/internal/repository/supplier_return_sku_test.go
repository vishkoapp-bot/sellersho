package repository

import (
	"context"
	"testing"
)

func TestCartonDuplicateVariantsRejectedBeforeDatabase(t *testing.T) {
	repo := &SupplierRepository{}
	_, err := repo.CreateCartonShipment(context.Background(), "supplier", []CartonShipmentItem{{VariantID: "00000000-0000-4000-8000-000000000001", ExpectedQty: 1}, {VariantID: "00000000-0000-4000-8000-000000000001", ExpectedQty: 2}})
	if err == nil {
		t.Fatal("duplicate SKU lines accepted")
	}
}

func TestSupplierSKUReturnReplayCannotChangeIdentityOrQuantity(t *testing.T) {
	stored := SupplierReturnRequest{ProductID: "p", VariantID: "sku-a", RequestedQuantity: 2, CollectionMethod: "postal", Reason: "return"}
	if err := validateSupplierReturnReplay(stored, "p", "sku-a", 2, "postal", "return"); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []SupplierReturnRequest{{ProductID: "other", VariantID: "sku-a", RequestedQuantity: 2, CollectionMethod: "postal", Reason: "return"}, {ProductID: "p", VariantID: "sku-b", RequestedQuantity: 2, CollectionMethod: "postal", Reason: "return"}, {ProductID: "p", VariantID: "sku-a", RequestedQuantity: 3, CollectionMethod: "postal", Reason: "return"}, {ProductID: "p", VariantID: "sku-a", RequestedQuantity: 2, CollectionMethod: "supplier_collect", Reason: "return"}} {
		if err := validateSupplierReturnReplay(stored, changed.ProductID, changed.VariantID, changed.RequestedQuantity, changed.CollectionMethod, changed.Reason); err == nil {
			t.Fatal("idempotency key reused for another return payload")
		}
	}
}
