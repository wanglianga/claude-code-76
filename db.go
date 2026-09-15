package main

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

func initDB(dsn string) {
	var err error
	for i := 0; i < 30; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			break
		}
		log.Printf("waiting for database (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	log.Println("database connected")
}

func migrate() {
	if _, err := db.Exec(schemaSQL); err != nil {
		log.Fatalf("migrate failed: %v", err)
	}
	log.Println("schema migrated")
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS communities (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  address TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS teams (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  phone TEXT NOT NULL DEFAULT '',
  community_id BIGINT REFERENCES communities(id),
  team_id BIGINT REFERENCES teams(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chemicals (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  unit TEXT NOT NULL DEFAULT '升',
  stock DOUBLE PRECISION NOT NULL DEFAULT 0,
  safe_stock DOUBLE PRECISION NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS team_schedules (
  id BIGSERIAL PRIMARY KEY,
  team_id BIGINT NOT NULL REFERENCES teams(id),
  work_date DATE NOT NULL,
  shift TEXT NOT NULL DEFAULT 'allday',
  max_orders INT NOT NULL DEFAULT 5,
  UNIQUE(team_id, work_date, shift)
);

CREATE TABLE IF NOT EXISTS rainfall_records (
  id BIGSERIAL PRIMARY KEY,
  community_id BIGINT NOT NULL REFERENCES communities(id),
  rain_date DATE NOT NULL,
  amount_mm DOUBLE PRECISION NOT NULL DEFAULT 0,
  UNIQUE(community_id, rain_date)
);

CREATE TABLE IF NOT EXISTS reports (
  id BIGSERIAL PRIMARY KEY,
  report_no TEXT NOT NULL DEFAULT '',
  reporter_id BIGINT NOT NULL REFERENCES users(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  type TEXT NOT NULL,
  location_desc TEXT NOT NULL DEFAULT '',
  latitude DOUBLE PRECISION,
  longitude DOUBLE PRECISION,
  photos JSONB NOT NULL DEFAULT '[]',
  nearby_population TEXT NOT NULL DEFAULT '',
  has_pets BOOLEAN NOT NULL DEFAULT false,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reports_community ON reports(community_id, status);
CREATE INDEX IF NOT EXISTS idx_reports_created ON reports(created_at);

CREATE TABLE IF NOT EXISTS water_points (
  id BIGSERIAL PRIMARY KEY,
  community_id BIGINT NOT NULL REFERENCES communities(id),
  type TEXT NOT NULL,
  location_desc TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual',
  report_id BIGINT REFERENCES reports(id),
  status TEXT NOT NULL DEFAULT 'pending',
  larvae_found BOOLEAN NOT NULL DEFAULT false,
  is_key BOOLEAN NOT NULL DEFAULT false,
  key_reason TEXT NOT NULL DEFAULT '',
  discovered_by BIGINT REFERENCES users(id),
  last_treated_at TIMESTAMPTZ,
  last_recheck_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_water_points_community ON water_points(community_id, status);

CREATE TABLE IF NOT EXISTS work_orders (
  id BIGSERIAL PRIMARY KEY,
  order_no TEXT NOT NULL DEFAULT '',
  community_id BIGINT NOT NULL REFERENCES communities(id),
  water_point_id BIGINT REFERENCES water_points(id),
  report_id BIGINT REFERENCES reports(id),
  team_id BIGINT REFERENCES teams(id),
  priority DOUBLE PRECISION NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  scheduled_date DATE,
  recheck_due_at TIMESTAMPTZ,
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_work_orders_community ON work_orders(community_id, status);
CREATE INDEX IF NOT EXISTS idx_work_orders_team ON work_orders(team_id, scheduled_date);

CREATE TABLE IF NOT EXISTS work_order_parties (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  party_role TEXT NOT NULL,
  user_id BIGINT REFERENCES users(id),
  note TEXT NOT NULL DEFAULT '',
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(work_order_id, party_role, user_id)
);

CREATE TABLE IF NOT EXISTS work_order_logs (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  actor_id BIGINT REFERENCES users(id),
  actor_role TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_logs_order ON work_order_logs(work_order_id);

CREATE TABLE IF NOT EXISTS treatments (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  operator_id BIGINT NOT NULL REFERENCES users(id),
  chemical_id BIGINT NOT NULL REFERENCES chemicals(id),
  chemical_name TEXT NOT NULL DEFAULT '',
  concentration TEXT NOT NULL DEFAULT '',
  spray_area TEXT NOT NULL DEFAULT '',
  warning_sign BOOLEAN NOT NULL DEFAULT false,
  resident_notified BOOLEAN NOT NULL DEFAULT false,
  pet_avoided BOOLEAN NOT NULL DEFAULT false,
  new_water_point_found BOOLEAN NOT NULL DEFAULT false,
  new_water_point_desc TEXT NOT NULL DEFAULT '',
  chemical_used DOUBLE PRECISION NOT NULL DEFAULT 0,
  treated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rechecks (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  water_point_id BIGINT REFERENCES water_points(id),
  inspector_id BIGINT NOT NULL REFERENCES users(id),
  result TEXT NOT NULL,
  larvae_found BOOLEAN NOT NULL DEFAULT false,
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS issues (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  type TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open',
  raised_by BIGINT REFERENCES users(id),
  handled_by BIGINT REFERENCES users(id),
  resolution TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS rectifications (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  water_point_id BIGINT REFERENCES water_points(id),
  property_user_id BIGINT NOT NULL REFERENCES users(id),
  description TEXT NOT NULL DEFAULT '',
  deadline DATE,
  status TEXT NOT NULL DEFAULT 'pending',
  completed_at TIMESTAMPTZ,
  verify_result TEXT NOT NULL DEFAULT '',
  verified_by BIGINT REFERENCES users(id),
  verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notifications (
  id BIGSERIAL PRIMARY KEY,
  work_order_id BIGINT REFERENCES work_orders(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  title TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT 'app',
  sent_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS risk_periods (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  disease TEXT NOT NULL DEFAULT '登革热',
  start_date DATE NOT NULL,
  end_date DATE NOT NULL,
  recheck_interval_days INT NOT NULL DEFAULT 3,
  active BOOLEAN NOT NULL DEFAULT false,
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`
