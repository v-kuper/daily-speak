-- This ledger deliberately has no foreign key to recordings: deleting learning
-- materials cannot remove earned practice time.
CREATE TABLE activity_events (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('speaking', 'review')),
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ NOT NULL CHECK (ended_at > started_at),
  PRIMARY KEY (user_id, id)
);

CREATE TABLE activity_credits (
  user_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  piece INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('speaking', 'review')),
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ NOT NULL CHECK (ended_at > started_at),
  historical BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (user_id, event_id, piece),
  FOREIGN KEY (user_id, event_id) REFERENCES activity_events(user_id, id) ON DELETE CASCADE
);
CREATE INDEX activity_credits_user_time_idx ON activity_credits(user_id, started_at, ended_at);

-- Older clients stored whole recording duration, with no listening/idle/review
-- breakdown. Preserve that available history once; do not invent review time.
INSERT INTO activity_events(user_id,id,kind,started_at,ended_at)
SELECT user_id,'history:'||id,'speaking',
  timestamp - make_interval(secs => LEAST(duration,600)), timestamp
FROM recordings WHERE duration > 0;

INSERT INTO activity_credits(user_id,event_id,piece,kind,started_at,ended_at,historical)
SELECT user_id,id,0,kind,started_at,ended_at,TRUE FROM activity_events;
