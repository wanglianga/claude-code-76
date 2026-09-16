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

CREATE TABLE IF NOT EXISTS child_zones (
  id BIGSERIAL PRIMARY KEY,
  community_id BIGINT NOT NULL REFERENCES communities(id),
  name TEXT NOT NULL,
  zone_type TEXT NOT NULL DEFAULT 'kindergarten',
  contact_name TEXT NOT NULL DEFAULT '',
  contact_phone TEXT NOT NULL DEFAULT '',
  contact_user_id BIGINT REFERENCES users(id),
  activity_times TEXT NOT NULL DEFAULT '',
  parent_group TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS child_zone_plans (
  id BIGSERIAL PRIMARY KEY,
  plan_no TEXT NOT NULL DEFAULT '',
  work_order_id BIGINT REFERENCES work_orders(id),
  child_zone_id BIGINT NOT NULL REFERENCES child_zones(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  planned_start TIMESTAMPTZ NOT NULL,
  planned_end TIMESTAMPTZ NOT NULL,
  wind_direction TEXT NOT NULL DEFAULT '',
  safety_interval_hours DOUBLE PRECISION NOT NULL DEFAULT 4,
  recovery_time TIMESTAMPTZ,
  contact_name TEXT NOT NULL DEFAULT '',
  contact_phone TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'planned',
  warning_removed_at TIMESTAMPTZ,
  warning_removed_by BIGINT REFERENCES users(id),
  confirmed_by BIGINT REFERENCES users(id),
  confirmed_at TIMESTAMPTZ,
  confirm_note TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cz_plans_zone ON child_zone_plans(child_zone_id, status);

CREATE TABLE IF NOT EXISTS zone_reminders (
  id BIGSERIAL PRIMARY KEY,
  plan_id BIGINT NOT NULL REFERENCES child_zone_plans(id),
  audience TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT 'app',
  title TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  avoid_period TEXT NOT NULL DEFAULT '',
  contact_info TEXT NOT NULL DEFAULT '',
  delivery_status TEXT NOT NULL DEFAULT 'pending',
  sent_at TIMESTAMPTZ,
  delivered_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reminders_plan ON zone_reminders(plan_id);

CREATE TABLE IF NOT EXISTS pet_complaints (
  id BIGSERIAL PRIMARY KEY,
  complaint_no TEXT NOT NULL DEFAULT '',
  work_order_id BIGINT REFERENCES work_orders(id),
  treatment_id BIGINT REFERENCES treatments(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  reporter_user_id BIGINT NOT NULL REFERENCES users(id),
  pet_type TEXT NOT NULL DEFAULT '',
  pet_name TEXT NOT NULL DEFAULT '',
  symptom TEXT NOT NULL DEFAULT '',
  walking_route TEXT NOT NULL DEFAULT '',
  chemical_id BIGINT REFERENCES chemicals(id),
  chemical_name TEXT NOT NULL DEFAULT '',
  spray_area TEXT NOT NULL DEFAULT '',
  warning_time TIMESTAMPTZ,
  medical_vouchers JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending',
  need_revisit BOOLEAN NOT NULL DEFAULT false,
  compensation BOOLEAN NOT NULL DEFAULT false,
  compensation_note TEXT NOT NULL DEFAULT '',
  chemical_note TEXT NOT NULL DEFAULT '',
  notify_adjustment TEXT NOT NULL DEFAULT '',
  resolution_note TEXT NOT NULL DEFAULT '',
  handled_by BIGINT REFERENCES users(id),
  handled_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pet_complaints_community ON pet_complaints(community_id, status, created_at);

-- ========== 物业积水整改（地下室排水沟长期积水等设施性积水） ==========
-- 消杀队对地下室排水沟长期积水只能临时处理，服务据此生成物业整改任务，
-- 记录排水维修、复查照片、居民投诉变化；超期影响物业考核，街道可跟进督办。
CREATE TABLE IF NOT EXISTS property_rectifications (
  id BIGSERIAL PRIMARY KEY,
  rect_no TEXT NOT NULL DEFAULT '',
  work_order_id BIGINT REFERENCES work_orders(id),
  water_point_id BIGINT REFERENCES water_points(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  -- 积水点信息
  water_location TEXT NOT NULL DEFAULT '',
  water_type TEXT NOT NULL DEFAULT 'basement_drain',
  -- 临时处理（消杀队）
  temp_treated_by BIGINT REFERENCES users(id),
  temp_treatment TEXT NOT NULL DEFAULT '',
  temp_treated_at TIMESTAMPTZ,
  temp_treatment_times INT NOT NULL DEFAULT 1,
  -- 设施责任人 + 复查日期
  facility_user_id BIGINT NOT NULL REFERENCES users(id),
  recheck_date DATE NOT NULL,
  -- 物业排水维修与整改照片（须标明积水点位置与处理方式）
  repair_desc TEXT NOT NULL DEFAULT '',
  repair_method TEXT NOT NULL DEFAULT '',
  rectify_photos JSONB NOT NULL DEFAULT '[]',
  rectify_photo_remark TEXT NOT NULL DEFAULT '',
  rectified_by BIGINT REFERENCES users(id),
  rectified_at TIMESTAMPTZ,
  -- 复查（复查人员按图核验）
  recheck_photos JSONB NOT NULL DEFAULT '[]',
  recheck_remark TEXT NOT NULL DEFAULT '',
  rechecked_by BIGINT REFERENCES users(id),
  rechecked_at TIMESTAMPTZ,
  recheck_result TEXT NOT NULL DEFAULT '',
  -- 居民投诉基线/当前（随同位置投诉自动统计）
  complaints_before INT NOT NULL DEFAULT 0,
  complaints_after INT NOT NULL DEFAULT 0,
  -- 状态 / 督办
  status TEXT NOT NULL DEFAULT 'pending',   -- pending|rectifying|recheck_pending|verified|rejected
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_prop_rect_community ON property_rectifications(community_id, status);
CREATE INDEX IF NOT EXISTS idx_prop_rect_facility ON property_rectifications(facility_user_id, status);

-- 街道/系统对整改、复查超期的督办提醒
CREATE TABLE IF NOT EXISTS property_rect_reminders (
  id BIGSERIAL PRIMARY KEY,
  rectification_id BIGINT NOT NULL REFERENCES property_rectifications(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'supervise',  -- rect_overdue 整改超期 | recheck_overdue 复查超期 | supervise 街道督办
  content TEXT NOT NULL DEFAULT '',
  raised_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_prop_rect_reminders ON property_rect_reminders(rectification_id);

-- ========== 重点风险期应急响应与跨小区联防调度 ==========
-- 小区风险等级（常态/风险升高/预警/应急）
ALTER TABLE communities ADD COLUMN IF NOT EXISTS risk_level TEXT NOT NULL DEFAULT 'normal';
ALTER TABLE communities ADD COLUMN IF NOT EXISTS risk_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE communities ADD COLUMN IF NOT EXISTS risk_updated_at TIMESTAMPTZ;

-- 工单关联应急响应（五方同单协同的应急工单）
ALTER TABLE work_orders ADD COLUMN IF NOT EXISTS emergency_response_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_orders_emerg ON work_orders(emergency_response_id);

-- 应急响应主表（一次疾控预警/病例/投诉突增触发，可覆盖多个联防小区）
CREATE TABLE IF NOT EXISTS emergency_responses (
  id BIGSERIAL PRIMARY KEY,
  emerg_no TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  trigger_type TEXT NOT NULL DEFAULT 'cdc_warning', -- cdc_warning|nearby_case|complaint_surge|manual
  disease TEXT NOT NULL DEFAULT '登革热',
  description TEXT NOT NULL DEFAULT '',
  recheck_interval_days INT NOT NULL DEFAULT 1,      -- 应急期间复查频次（天）
  status TEXT NOT NULL DEFAULT 'active',             -- active|resolved
  created_by BIGINT REFERENCES users(id),
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  resolve_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_emerg_status ON emergency_responses(status);

-- 病例与活动轨迹（疑似/确诊，关联到联防小区）
CREATE TABLE IF NOT EXISTS emergency_cases (
  id BIGSERIAL PRIMARY KEY,
  emergency_id BIGINT NOT NULL REFERENCES emergency_responses(id) ON DELETE CASCADE,
  community_id BIGINT REFERENCES communities(id),
  case_status TEXT NOT NULL DEFAULT 'suspect',       -- suspect|confirmed
  patient_alias TEXT NOT NULL DEFAULT '',            -- 隐私脱敏称谓
  onset_date DATE,
  trajectory JSONB NOT NULL DEFAULT '[]',            -- [{time, place, note}]
  source TEXT NOT NULL DEFAULT '疾控通报',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_emerg_cases ON emergency_cases(emergency_id);

-- 联防小区（主疫区/周边支援小区，各带风险等级与一张应急工单）
CREATE TABLE IF NOT EXISTS emergency_communities (
  id BIGSERIAL PRIMARY KEY,
  emergency_id BIGINT NOT NULL REFERENCES emergency_responses(id) ON DELETE CASCADE,
  community_id BIGINT NOT NULL REFERENCES communities(id),
  role TEXT NOT NULL DEFAULT 'affected',             -- affected 主疫区|surrounding 周边联防
  risk_level TEXT NOT NULL DEFAULT 'warning',
  risk_reason TEXT NOT NULL DEFAULT '',
  work_order_id BIGINT REFERENCES work_orders(id),
  complaints_at_start INT NOT NULL DEFAULT 0,
  UNIQUE(emergency_id, community_id)
);
CREATE INDEX IF NOT EXISTS idx_emerg_comm ON emergency_communities(emergency_id);

-- 重点积水点快照（楼顶水箱/地下车库/绿化带/雨水井/建筑工地等，关联病例轨迹）
CREATE TABLE IF NOT EXISTS emergency_water_points (
  id BIGSERIAL PRIMARY KEY,
  emergency_id BIGINT NOT NULL REFERENCES emergency_responses(id) ON DELETE CASCADE,
  community_id BIGINT NOT NULL REFERENCES communities(id),
  water_point_id BIGINT REFERENCES water_points(id),
  wp_type TEXT NOT NULL DEFAULT '',
  location_desc TEXT NOT NULL DEFAULT '',
  link_reason TEXT NOT NULL DEFAULT '',              -- 与病例轨迹/风险的关联说明
  status_snapshot TEXT NOT NULL DEFAULT '',
  is_key BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_emerg_wp ON emergency_water_points(emergency_id, community_id);

-- 跨小区联防调度（消杀队/药剂/网格/物业/卫监/跨队支援）
CREATE TABLE IF NOT EXISTS emergency_dispatch (
  id BIGSERIAL PRIMARY KEY,
  emergency_id BIGINT NOT NULL REFERENCES emergency_responses(id) ON DELETE CASCADE,
  from_community_id BIGINT REFERENCES communities(id), -- 支援方小区（跨小区支援时）
  to_community_id BIGINT NOT NULL REFERENCES communities(id),
  team_id BIGINT REFERENCES teams(id),
  resource_type TEXT NOT NULL DEFAULT 'team',        -- team|chemical|grid|property|supervisor
  resource_ref TEXT NOT NULL DEFAULT '',             -- 药剂名/人员说明
  amount DOUBLE PRECISION NOT NULL DEFAULT 0,
  action TEXT NOT NULL DEFAULT '',
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_emerg_dispatch ON emergency_dispatch(emergency_id);

-- 每日汇总（风险解除前：投诉变化/积水点清零/药剂消耗/整改责任）
CREATE TABLE IF NOT EXISTS emergency_daily_summaries (
  id BIGSERIAL PRIMARY KEY,
  emergency_id BIGINT NOT NULL REFERENCES emergency_responses(id) ON DELETE CASCADE,
  community_id BIGINT REFERENCES communities(id),     -- NULL 表示全响应汇总
  summary_date DATE NOT NULL,
  new_complaints INT NOT NULL DEFAULT 0,
  open_water_points INT NOT NULL DEFAULT 0,
  cleared_water_points INT NOT NULL DEFAULT 0,
  chemical_used DOUBLE PRECISION NOT NULL DEFAULT 0,
  treatments INT NOT NULL DEFAULT 0,
  rect_open INT NOT NULL DEFAULT 0,
  rect_overdue INT NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(emergency_id, community_id, summary_date)
);
CREATE INDEX IF NOT EXISTS idx_emerg_daily ON emergency_daily_summaries(emergency_id, summary_date);

-- ========== 居民拒绝入户与入户消杀授权处理 ==========
-- 居民拒绝入户时不强行派单，居民/物业/消杀队/街道/卫生监督在同一工单协商；
-- 授权/反悔/外围/风险延续全过程版本化保存。
CREATE TABLE IF NOT EXISTS access_cases (
  id BIGSERIAL PRIMARY KEY,
  case_no TEXT NOT NULL DEFAULT '',
  work_order_id BIGINT NOT NULL REFERENCES work_orders(id),
  community_id BIGINT NOT NULL REFERENCES communities(id),
  resident_user_id BIGINT NOT NULL REFERENCES users(id),
  address TEXT NOT NULL DEFAULT '',                  -- 户内位置（楼栋门牌等）

  -- 拒绝登记
  status TEXT NOT NULL DEFAULT 'refused',            -- refused|negotiating|mandatory_review|external_only|authorized|in_progress|withdrawn|completed|risk_continued
  reject_reasons JSONB NOT NULL DEFAULT '[]',        -- privacy|children|elderly|pregnant|pets|respiratory|distrust_chemical|other
  reject_note TEXT NOT NULL DEFAULT '',
  sensitive_groups JSONB NOT NULL DEFAULT '[]',      -- children|elderly|pregnant|respiratory
  pets_desc TEXT NOT NULL DEFAULT '',
  acceptable_times TEXT NOT NULL DEFAULT '',
  acceptable_chemicals TEXT NOT NULL DEFAULT '',
  outdoor_allowed BOOLEAN NOT NULL DEFAULT false,
  outdoor_areas JSONB NOT NULL DEFAULT '[]',         -- doorway|staircase|balcony|sewer
  recorded_by BIGINT REFERENCES users(id),
  rejected_at TIMESTAMPTZ,

  -- 多方沟通 / 卫监强制入户评估（重点风险期或周边疑似病例）
  mandatory_required BOOLEAN NOT NULL DEFAULT false,
  assess_by BIGINT REFERENCES users(id),
  assess_note TEXT NOT NULL DEFAULT '',
  assessed_at TIMESTAMPTZ,
  witnesses TEXT NOT NULL DEFAULT '',
  final_opinion TEXT NOT NULL DEFAULT '',

  -- 居民授权快照
  auth_scope TEXT NOT NULL DEFAULT '',
  auth_chemical_name TEXT NOT NULL DEFAULT '',
  auth_concentration TEXT NOT NULL DEFAULT '',
  auth_safety_interval_hours DOUBLE PRECISION NOT NULL DEFAULT 0,
  auth_item_cover BOOLEAN NOT NULL DEFAULT false,
  auth_pet_avoid BOOLEAN NOT NULL DEFAULT false,
  auth_vulnerable_avoid BOOLEAN NOT NULL DEFAULT false,
  auth_companion TEXT NOT NULL DEFAULT '',
  auth_photo_consent BOOLEAN NOT NULL DEFAULT false,
  auth_notice_delivered BOOLEAN NOT NULL DEFAULT false,
  authorized_at TIMESTAMPTZ,

  -- 临时反悔 / 改约（药剂与排班保留或改约）
  withdraw_reason TEXT NOT NULL DEFAULT '',
  reschedule_date DATE,
  resources_kept BOOLEAN NOT NULL DEFAULT true,
  withdrawn_at TIMESTAMPTZ,

  -- 仅外围公共区域处理（关联物业责任与复查）
  external_rectification_id BIGINT REFERENCES property_rectifications(id),
  external_note TEXT NOT NULL DEFAULT '',

  -- 入户作业完成
  spray_area TEXT NOT NULL DEFAULT '',
  warning_sign BOOLEAN NOT NULL DEFAULT false,
  warning_removed_at TIMESTAMPTZ,
  completion_photos JSONB NOT NULL DEFAULT '[]',
  resident_confirmed BOOLEAN NOT NULL DEFAULT false,
  pet_avoid_done BOOLEAN NOT NULL DEFAULT false,
  child_safety_interval_hours DOUBLE PRECISION NOT NULL DEFAULT 0,
  completed_by BIGINT REFERENCES users(id),
  completed_at TIMESTAMPTZ,

  -- 因拒绝入户无法根治 → 风险延续（提高公共区域复查频次，进入考核/计划）
  risk_continued BOOLEAN NOT NULL DEFAULT false,
  risk_note TEXT NOT NULL DEFAULT '',
  recheck_interval_days INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_access_order ON access_cases(work_order_id);
CREATE INDEX IF NOT EXISTS idx_access_resident ON access_cases(resident_user_id, status);
CREATE INDEX IF NOT EXISTS idx_access_community ON access_cases(community_id, status);

-- 全量版本化：拒绝/沟通/评估/授权/变更/反悔/延期/外围/完成/风险延续
CREATE TABLE IF NOT EXISTS access_case_versions (
  id BIGSERIAL PRIMARY KEY,
  case_id BIGINT NOT NULL REFERENCES access_cases(id) ON DELETE CASCADE,
  version_no INT NOT NULL,
  action TEXT NOT NULL,
  actor_id BIGINT REFERENCES users(id),
  actor_role TEXT NOT NULL DEFAULT '',
  snapshot JSONB NOT NULL DEFAULT '{}',
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_access_versions ON access_case_versions(case_id, version_no);
`
