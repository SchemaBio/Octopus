package model

import (
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestIGVSnapshotTenantColumnSupportsOrganizationScope(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost", PreferSimpleProtocol: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := schema.Parse(&IGVSnapshot{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	task, err := schema.Parse(&Task{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	field := snapshot.LookUpField("TenantID")
	tenant := TenantIDForTask(&Task{ExternalOrgID: "0b99dc67-70b5-4f8b-b1db-a9d2298d141d"})
	if field.Size < len(tenant) {
		t.Fatalf("snapshot tenant column size %d cannot store %q", field.Size, tenant)
	}
	got := db.Dialector.DataTypeOf(field)
	want := db.Dialector.DataTypeOf(task.LookUpField("TenantID"))
	if got != want {
		t.Fatalf("snapshot tenant column type %q differs from task tenant column %q", got, want)
	}
}
