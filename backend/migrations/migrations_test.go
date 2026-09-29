package migrations

import (
	"strings"
	"testing"
)

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

func TestRecordingDurationPolicyMigrationUpdatesActiveGuestAndAccountSessions(t *testing.T) {
	items, err := All()
	if err != nil {
		t.Fatal(err)
	}
	var policy Migration
	for _, item := range items {
		if item.Name == "0013_recording_duration_policy.sql" {
			policy = item
			break
		}
	}
	if policy.Name == "" {
		t.Fatal("recording duration policy migration is missing")
	}
	for _, fragment := range []string{
		"WHEN user_id IS NULL THEN 180 ELSE 600 END",
		"'preparing', 'ready', 'recording', 'finalizing'",
	} {
		if !strings.Contains(policy.SQL, fragment) {
			t.Fatalf("duration policy migration is missing %q", fragment)
		}
	}
}

func TestInterviewVocabularyMigrationKeepsTranslationsSeparate(t *testing.T) {
	items, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name != "0015_interview_vocabulary_translations.sql" {
			continue
		}
		if !strings.Contains(item.SQL, "useful_vocabulary JSONB NOT NULL DEFAULT '[]'::jsonb") {
			t.Fatalf("vocabulary migration does not add the translated vocabulary column: %s", item.SQL)
		}
		return
	}
	t.Fatal("interview vocabulary migration is missing")
}

func TestInterviewSkippedTurnsMigrationPreservesSequenceHistory(t *testing.T) {
	items, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name != "0016_interview_skipped_turns.sql" {
			continue
		}
		for _, fragment := range []string{
			"skipped BOOLEAN NOT NULL DEFAULT FALSE",
			"skip_key TEXT",
			"interview_turns_skip_key_idx",
		} {
			if !strings.Contains(item.SQL, fragment) {
				t.Fatalf("skipped-turn migration is missing %q: %s", fragment, item.SQL)
			}
		}
		return
	}
	t.Fatal("interview skipped-turn migration is missing")
}

func TestRecordingStrengthsMigrationAddsDurableResult(t *testing.T) {
	items, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Name == "0017_recording_strengths.sql" {
			if !strings.Contains(item.SQL, "strengths JSONB NOT NULL DEFAULT '[]'::jsonb") {
				t.Fatalf("strengths migration is incomplete: %s", item.SQL)
			}
			return
		}
	}
	t.Fatal("recording strengths migration is missing")
}
