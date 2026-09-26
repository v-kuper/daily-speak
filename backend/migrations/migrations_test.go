package migrations

import "testing"

func TestRejectTransactionControl(t *testing.T) {
	allowed := []string{
		InitialSchema,
		"SELECT 'COMMIT;'; -- ROLLBACK;\nSELECT 1;",
		"SELECT E'it\\'s; COMMIT;';",
		"DO $$ BEGIN RAISE NOTICE 'COMMIT'; END $$;",
		"/* COMMIT; */ SELECT 1;",
	}
	for _, sql := range allowed {
		if err := rejectTransactionControl(sql); err != nil {
			t.Fatalf("rejected valid SQL: %v", err)
		}
	}
	blocked := []string{
		"COMMIT;",
		"SELECT 1; ROLLBACK;",
		"DO $$ BEGIN NULL; END $$; START TRANSACTION;",
		"/* comment */ BEGIN; SELECT 1;",
		"END;",
		"ABORT;",
		"PREPARE TRANSACTION 'x';",
	}
	for _, sql := range blocked {
		if err := rejectTransactionControl(sql); err == nil {
			t.Fatalf("accepted transaction control: %q", sql)
		}
	}
}
