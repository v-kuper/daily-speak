package shadowing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"daily-speaking-practice/backend/internal/db"
	"github.com/google/uuid"
)

func TestStoreReplacesExperimentalSampleOnceWithLearnerAudio(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	database, err := db.Connect(ctx, url, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	owner, recordingID, sessionID, oldAsset := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	t.Cleanup(func() {
		_, _ = database.Exec(context.Background(), `DELETE FROM processing_jobs WHERE resource_id=$1`, recordingID)
		_, _ = database.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, owner)
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,email,password_hash,english_level) VALUES($1,$2,'integration-test','a2')`, owner, "shadow-sample-"+owner+"@example.com")
	exec(`INSERT INTO media_assets(id,owner_principal_id,purpose,state,storage_driver,object_key,content_type,expected_size_bytes,verified_size_bytes,expected_checksum_sha256,verified_checksum_sha256,attached_at) VALUES($1,$2,'shadowing_audio','ready','local',$3,'audio/mpeg',3,3,$4,$4,NOW())`, oldAsset, owner, "shadow-test/"+oldAsset, strings.Repeat("a", 64))
	exec(`INSERT INTO recordings(id,user_id,topic,duration,timestamp,transcript,corrected_transcript,status,shadowing_status,shadowing_asset_id) VALUES($1,$2,'Travel',20,NOW(),'Private hospital answer.','Where did you go? I went to a hospital.','ready','ready',$3)`, recordingID, owner, oldAsset)
	exec(`INSERT INTO interview_sessions(id,owner_principal_id,create_key,request_digest,user_id,topic,opening_question,status,max_duration_seconds,recording_id,english_level) VALUES($1,$2,$3,$4,$2,'Travel','Where did you go?','finalized',600,$5,'c1')`, sessionID, owner, uuid.NewString(), uuid.NewString(), recordingID)
	exec(`INSERT INTO interview_turns(id,session_id,seq,question,asked_at_ms,ended_at_ms,final_transcript,transcript_status,transcript_origin,skipped) VALUES($1,$3,1,'Where did you go?',0,1000,'Private hospital answer.','ready','turn_realtime',false),($2,$3,2,'Skipped question?',1000,2000,'','pending',NULL,true)`, uuid.NewString(), uuid.NewString(), sessionID)
	exec(`UPDATE recordings SET shadowing_script=$2::jsonb WHERE id=$1`, recordingID, `{"englishLevel":"b1","text":"Where did you go? I went to a park.","turns":[{"sequence":1,"question":"Where did you go?","answerText":"I went to a park."}]}`)
	store := NewStore(database)
	if _, err := store.Schedule(ctx, uuid.NewString(), recordingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	if started, err := store.Schedule(ctx, owner, recordingID); err != nil || !started {
		t.Fatalf("upgrade started=%v err=%v", started, err)
	}
	if started, err := store.Schedule(ctx, owner, recordingID); err != nil || started {
		t.Fatalf("duplicate started=%v err=%v", started, err)
	}
	job := Job{ResourceID: recordingID, LeaseToken: uuid.NewString()}
	if err := database.QueryRow(ctx, `UPDATE processing_jobs SET state='running',lease_token=$2,lease_expires_at=NOW()+INTERVAL '5 minutes' WHERE resource_id=$1 RETURNING id`, recordingID, job.LeaseToken).Scan(&job.ID); err != nil {
		t.Fatal(err)
	}
	work, found, err := store.LoadWork(ctx, job)
	if err != nil || !found || work.CorrectedTranscript != "Where did you go? I went to a hospital." {
		t.Fatalf("work=%#v found=%v err=%v", work, found, err)
	}
	stale := job
	stale.LeaseToken = uuid.NewString()
	if _, found, err := store.LoadWork(ctx, stale); err != nil || found {
		t.Fatalf("stale work found=%v err=%v", found, err)
	}
	var cleared bool
	if err := database.QueryRow(ctx, `SELECT shadowing_script IS NULL FROM recordings WHERE id=$1`, recordingID).Scan(&cleared); err != nil || !cleared {
		t.Fatalf("experimental script retained: %v", err)
	}
	asset := Asset{ID: uuid.NewString(), OwnerID: owner, StorageDriver: "local", ObjectKey: "shadow-test/" + uuid.NewString(), Size: 3, Checksum: strings.Repeat("b", 64)}
	if completed, err := store.Complete(ctx, stale, asset); err != nil || completed {
		t.Fatalf("stale completion=%v err=%v", completed, err)
	}
	if completed, err := store.Complete(ctx, job, asset); err != nil || !completed {
		t.Fatalf("completion=%v err=%v", completed, err)
	}
	var retired bool
	if err := database.QueryRow(ctx, `SELECT attached_at IS NULL AND retention_until IS NOT NULL FROM media_assets WHERE id=$1`, oldAsset).Scan(&retired); err != nil || !retired {
		t.Fatalf("old asset retired=%v err=%v", retired, err)
	}
	if started, err := store.Schedule(ctx, owner, recordingID); err != nil || started {
		t.Fatalf("ready sample regenerated=%v err=%v", started, err)
	}
	var original, corrected string
	if err := database.QueryRow(ctx, `SELECT transcript,corrected_transcript FROM recordings WHERE id=$1`, recordingID).Scan(&original, &corrected); err != nil || original != "Private hospital answer." || corrected != "Where did you go? I went to a hospital." {
		t.Fatalf("learner material changed: %q %q err=%v", original, corrected, err)
	}
}
