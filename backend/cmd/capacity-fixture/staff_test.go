package main

import (
	"database/sql"
	"testing"
)

func seedAndVerifyStaff(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role)
 SELECT md5('cap-staff-'||i)::uuid,'096'||lpad(i::text,8,'0'),'cap-staff-'||i,CASE WHEN i<=5 THEN 'warehouse_staff' ELSE 'admin' END FROM generate_series(1,10)i;
 INSERT INTO user_capabilities(user_id,capability,source_type) SELECT id,role,'capacity_fixture' FROM users WHERE full_name LIKE 'cap-staff-%'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE full_name LIKE 'cap-staff-%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("staff fixture principals=%d want10", count)
	}
}
