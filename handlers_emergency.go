package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func registerEmergencyRoutes() {
	handle("POST /api/emergencies", hCreateEmergency, "street")
	handle("GET /api/emergencies", hListEmergencies)
	handle("GET /api/emergencies/{id}", hGetEmergency)
	handle("POST /api/emergencies/{id}/dispatch", hEmergencyDispatch, "street")
	handle("POST /api/emergencies/{id}/daily-summary", hEmergencyDailySummary, "street")
	handle("POST /api/emergencies/{id}/resolve", hResolveEmergency, "street")
	handle("GET /api/emergencies/risk/communities", hRiskCommunities)
}

// 应急期间纳入快照的重点积水点类型（楼顶水箱、地下车库、绿化带、雨水井、建筑工地等）
var emergKeyWpTypes = map[string]bool{
	"rooftop_tank": true, "rooftop_water": true, "underground_garage": true,
	"basement_damp": true, "rain_well": true, "construction_site": true,
	"greenbelt_water": true, "greenbelt_bush": true, "waste_tire": true,
}

const (
	emergSurgeWindowHours = 72 // 投诉突增观察窗口
	emergSurgeThreshold   = 4  // 窗口内投诉数阈值
)

// ---------- 风险等级自动提升 ----------

// maybeElevateCommunityRisk 疾控预警/病例由街道建应急时提升；投诉密度突增在此自动提升小区风险等级
func maybeElevateCommunityRisk(communityID int64) {
	var n int
	db.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1 AND created_at > now() - ($2 || ' hours')::interval`,
		communityID, emergSurgeWindowHours).Scan(&n)
	if n < emergSurgeThreshold {
		return
	}
	var level string
	db.QueryRow(`SELECT risk_level FROM communities WHERE id=$1`, communityID).Scan(&level)
	if level == "elevated" || level == "warning" || level == "emergency" {
		return // 已处于更高/同级管控，不降级覆盖
	}
	db.Exec(`UPDATE communities SET risk_level='elevated', risk_reason=$1, risk_updated_at=now() WHERE id=$2`,
		fmt.Sprintf("近 %d 小时投诉密度突增（%d 件），系统自动提升风险等级", emergSurgeWindowHours, n), communityID)
	cacheDel("dashboard:closedloop", "emerg:active")
}

// activeEmergencyLevel 返回小区当前生效应急的最高复查间隔（天）与是否在应急中
func emergencyRecheckInterval(communityID int64) (int, bool) {
	var iv int
	err := db.QueryRow(`SELECT MIN(e.recheck_interval_days) FROM emergency_responses e
		JOIN emergency_communities ec ON ec.emergency_id=e.id
		WHERE e.status='active' AND ec.community_id=$1`, communityID).Scan(&iv)
	if err != nil || iv <= 0 {
		return 0, false
	}
	return iv, true
}

// ---------- 创建应急响应 ----------

type emergCommunityReq struct {
	CommunityID int64  `json:"community_id"`
	Role        string `json:"role"` // affected|surrounding
	Reason      string `json:"reason"`
}
type emergCaseReq struct {
	CommunityID int64           `json:"community_id"`
	CaseStatus  string          `json:"case_status"`
	PatientAlias string         `json:"patient_alias"`
	OnsetDate   string          `json:"onset_date"`
	Trajectory  []map[string]any `json:"trajectory"`
	Source      string          `json:"source"`
}

func hCreateEmergency(c *Ctx) {
	var req struct {
		Title              string                `json:"title"`
		TriggerType        string                `json:"trigger_type"`
		Disease            string                `json:"disease"`
		Description        string                `json:"description"`
		RecheckIntervalDays int                  `json:"recheck_interval_days"`
		Communities        []emergCommunityReq   `json:"communities"`
		Cases              []emergCaseReq        `json:"cases"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Title == "" {
		jsonErr(c.W, 400, "请填写应急标题")
		return
	}
	if _, ok := EmergTriggerLabels[req.TriggerType]; !ok {
		req.TriggerType = "manual"
	}
	if req.Disease == "" {
		req.Disease = "登革热"
	}
	if req.RecheckIntervalDays <= 0 {
		req.RecheckIntervalDays = 1
	}
	if len(req.Communities) == 0 {
		jsonErr(c.W, 400, "请至少选择一个联防小区")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer tx.Rollback()

	var eid int64
	if err := tx.QueryRow(`INSERT INTO emergency_responses(title, trigger_type, disease, description, recheck_interval_days, created_by)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		req.Title, req.TriggerType, req.Disease, req.Description, req.RecheckIntervalDays, c.User.ID).Scan(&eid); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	tx.Exec(`UPDATE emergency_responses SET emerg_no='EM'||LPAD(id::text,8,'0') WHERE id=$1`, eid)

	for _, ec := range req.Communities {
		var cname string
		if err := tx.QueryRow(`SELECT name FROM communities WHERE id=$1`, ec.CommunityID).Scan(&cname); err != nil {
			jsonErr(c.W, 400, "小区不存在")
			return
		}
		role := ec.Role
		if role != "surrounding" {
			role = "affected"
		}
		riskLevel := "warning"
		if role == "affected" {
			riskLevel = "emergency"
		}
		reason := ec.Reason
		if reason == "" {
			reason = labelOf(EmergTriggerLabels, req.TriggerType)
		}
		tx.Exec(`UPDATE communities SET risk_level=$1, risk_reason=$2, risk_updated_at=now() WHERE id=$3`, riskLevel, reason, ec.CommunityID)

		var startComplaints int
		tx.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1`, ec.CommunityID).Scan(&startComplaints)

		// 每个联防小区一张应急协同工单（五方同单）
		var oid int64
		if err := tx.QueryRow(`INSERT INTO work_orders(community_id, priority, status, created_by, emergency_response_id)
			VALUES($1,100,'escalated',$2,$3) RETURNING id`, ec.CommunityID, c.User.ID, eid).Scan(&oid); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		tx.Exec(`UPDATE work_orders SET order_no='WO'||LPAD(id::text,8,'0') WHERE id=$1`, oid)

		if _, err := tx.Exec(`INSERT INTO emergency_communities(emergency_id, community_id, role, risk_level, risk_reason, work_order_id, complaints_at_start)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, eid, ec.CommunityID, role, riskLevel, reason, oid, startComplaints); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		// 五方拉通 + 应急时间线（独立 DB 句柄，addLog/ensureAllParties 不在 tx 内，提交后补）
		_ = oid
	}

	// 病例与轨迹
	for _, cs := range req.Cases {
		if cs.CommunityID == 0 {
			continue
		}
		status := cs.CaseStatus
		if status != "confirmed" {
			status = "suspect"
		}
		source := cs.Source
		if source == "" {
			source = "疾控通报"
		}
		var onset any
		if cs.OnsetDate != "" {
			onset = cs.OnsetDate
		}
		traj := cs.Trajectory
		if traj == nil {
			traj = []map[string]any{}
		}
		trajJSON, _ := json.Marshal(traj)
		tx.Exec(`INSERT INTO emergency_cases(emergency_id, community_id, case_status, patient_alias, onset_date, trajectory, source)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, eid, cs.CommunityID, status, cs.PatientAlias, onset, RawJSON(trajJSON), source)
	}
	if err := tx.Commit(); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}

	// 提交后：建积水点快照、五方协同与应急时间线
	communityIDs := []int64{}
	orderIDs := map[int64]int64{} // community -> order
	rows, _ := db.Query(`SELECT community_id, work_order_id FROM emergency_communities WHERE emergency_id=$1`, eid)
	for rows.Next() {
		var cid, oid int64
		rows.Scan(&cid, &oid)
		communityIDs = append(communityIDs, cid)
		orderIDs[cid] = oid
	}
	rows.Close()

	casePlaces := snapshotEmergencyWaterPoints(eid, communityIDs, req.Cases)

	for _, cid := range communityIDs {
		oid := orderIDs[cid]
		ensureAllParties(oid)
		addLog(oid, c.User, "启动应急响应",
			fmt.Sprintf("【%s·%s】%s；复查频次提高为每 %d 天一次，居民/物业/消杀队/街道/卫生监督同单协同",
				req.Disease, labelOf(EmergTriggerLabels, req.TriggerType), req.Title, req.RecheckIntervalDays))
		if req.Description != "" {
			addLog(oid, c.User, "应急情况说明", req.Description)
		}
	}
	cacheDel("dashboard:closedloop", "emerg:active")
	jsonOK(c.W, map[string]any{
		"id": eid, "emerg_no": fmt.Sprintf("EM%08d", eid),
		"message": fmt.Sprintf("应急响应已启动，覆盖 %d 个联防小区，重点积水点 %d 处已纳入清单", len(communityIDs), casePlaces),
	})
}

// snapshotEmergencyWaterPoints 为各联防小区生成重点积水点快照，返回命中轨迹场所的数量
func snapshotEmergencyWaterPoints(eid int64, communityIDs []int64, cases []emergCaseReq) int {
	trajPlaces := map[string]bool{}
	for _, cs := range cases {
		for _, t := range cs.Trajectory {
			if place, ok := t["place"].(string); ok && place != "" {
				trajPlaces[normWaterLoc(place)] = true
			}
		}
	}
	hit := 0
	for _, cid := range communityIDs {
		rows, err := db.Query(`SELECT id, type, location_desc, status, is_key, larvae_found FROM water_points
			WHERE community_id=$1 AND status != 'cleared' ORDER BY is_key DESC, id`, cid)
		if err != nil {
			continue
		}
		type wp struct {
			id                             int64
			typ, loc, status               string
			isKey, larvae                  bool
		}
		var ps []wp
		for rows.Next() {
			var x wp
			rows.Scan(&x.id, &x.typ, &x.loc, &x.status, &x.isKey, &x.larvae)
			ps = append(ps, x)
		}
		rows.Close()
		for _, w := range ps {
			reason := ""
			include := false
			if emergKeyWpTypes[w.typ] {
				include = true
				reason = "重点类型：" + labelOf(WaterPointTypes, w.typ)
			}
			if w.isKey {
				include = true
				reason = addReason(reason, "重点积水点")
			}
			if w.larvae {
				include = true
				reason = addReason(reason, "曾发现幼虫")
			}
			if trajPlaces[normWaterLoc(w.loc)] {
				include = true
				reason = addReason(reason, "邻近病例活动轨迹")
				hit++
			}
			if !include {
				continue
			}
			db.Exec(`INSERT INTO emergency_water_points(emergency_id, community_id, water_point_id, wp_type, location_desc, link_reason, status_snapshot, is_key)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
				eid, cid, w.id, w.typ, w.loc, reason, w.status, w.isKey || w.larvae)
		}
	}
	return hit
}

func addReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + "；" + b
}

// ---------- 列表 / 详情 ----------

func canViewEmergency(u *SessionUser, eid int64) bool {
	switch u.Role {
	case "street", "supervisor":
		return true
	case "property", "grid", "resident":
		if u.CommunityID == nil {
			return false
		}
		var n int
		db.QueryRow(`SELECT count(*) FROM emergency_communities WHERE emergency_id=$1 AND community_id=$2`, eid, *u.CommunityID).Scan(&n)
		return n > 0
	case "operator":
		if u.TeamID == nil {
			return false
		}
		var n int
		db.QueryRow(`SELECT count(*) FROM emergency_dispatch WHERE emergency_id=$1 AND team_id=$2`, eid, *u.TeamID).Scan(&n)
		if n > 0 {
			return true
		}
		db.QueryRow(`SELECT count(*) FROM work_orders WHERE emergency_response_id=$1 AND team_id=$2`, eid, *u.TeamID).Scan(&n)
		return n > 0
	}
	return false
}

func hListEmergencies(c *Ctx) {
	q := `SELECT e.id, e.emerg_no, e.title, e.trigger_type, e.disease, e.status, e.recheck_interval_days,
		e.started_at, e.resolved_at,
		(SELECT count(*) FROM emergency_communities ec WHERE ec.emergency_id=e.id),
		(SELECT count(*) FROM emergency_water_points ew WHERE ew.emergency_id=e.id)
		FROM emergency_responses e WHERE 1=1`
	args := []any{}
	if c.User.Role != "street" && c.User.Role != "supervisor" {
		switch c.User.Role {
		case "property", "grid", "resident":
			if c.User.CommunityID != nil {
				q += ` AND e.id IN (SELECT emergency_id FROM emergency_communities WHERE community_id=$1)`
				args = append(args, *c.User.CommunityID)
			} else {
				q += ` AND false`
			}
		case "operator":
			if c.User.TeamID != nil {
				q += ` AND e.id IN (
					SELECT emergency_response_id FROM work_orders WHERE emergency_response_id IS NOT NULL AND team_id=$1
					UNION SELECT emergency_id FROM emergency_dispatch WHERE team_id=$1)`
				args = append(args, *c.User.TeamID)
			} else {
				q += ` AND false`
			}
		default:
			q += ` AND false`
		}
	}
	q += ` ORDER BY e.status='active' DESC, e.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type E struct {
		ID               int64      `json:"id"`
		EmergNo          string     `json:"emerg_no"`
		Title            string     `json:"title"`
		TriggerType      string     `json:"trigger_type"`
		TriggerLabel     string     `json:"trigger_label"`
		Disease          string     `json:"disease"`
		Status           string     `json:"status"`
		RecheckInterval  int        `json:"recheck_interval_days"`
		StartedAt        time.Time  `json:"started_at"`
		ResolvedAt       *time.Time `json:"resolved_at"`
		CommunityCount   int        `json:"community_count"`
		WaterPointCount  int        `json:"water_point_count"`
	}
	list := []E{}
	for rows.Next() {
		var x E
		rows.Scan(&x.ID, &x.EmergNo, &x.Title, &x.TriggerType, &x.Disease, &x.Status, &x.RecheckInterval,
			&x.StartedAt, &x.ResolvedAt, &x.CommunityCount, &x.WaterPointCount)
		x.TriggerLabel = labelOf(EmergTriggerLabels, x.TriggerType)
		list = append(list, x)
	}
	jsonOK(c.W, list)
}

func hGetEmergency(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var e struct {
		ID                  int64      `json:"id"`
		EmergNo             string     `json:"emerg_no"`
		Title               string     `json:"title"`
		TriggerType         string     `json:"trigger_type"`
		TriggerLabel        string     `json:"trigger_label"`
		Disease             string     `json:"disease"`
		Description         string     `json:"description"`
		RecheckIntervalDays int        `json:"recheck_interval_days"`
		Status              string     `json:"status"`
		StartedAt           time.Time  `json:"started_at"`
		ResolvedAt          *time.Time `json:"resolved_at"`
		ResolveNote         string     `json:"resolve_note"`
	}
	err := db.QueryRow(`SELECT id, emerg_no, title, trigger_type, disease, description, recheck_interval_days, status, started_at, resolved_at, resolve_note
		FROM emergency_responses WHERE id=$1`, id).
		Scan(&e.ID, &e.EmergNo, &e.Title, &e.TriggerType, &e.Disease, &e.Description, &e.RecheckIntervalDays,
			&e.Status, &e.StartedAt, &e.ResolvedAt, &e.ResolveNote)
	if err != nil {
		jsonErr(c.W, 404, "应急响应不存在")
		return
	}
	if !canViewEmergency(c.User, id) {
		jsonErr(c.W, 403, "无权查看该应急响应")
		return
	}
	e.TriggerLabel = labelOf(EmergTriggerLabels, e.TriggerType)

	// 病例与轨迹
	type EmergCase struct {
		ID            int64            `json:"id"`
		CommunityName string           `json:"community_name"`
		CaseStatus    string           `json:"case_status"`
		StatusLabel   string           `json:"status_label"`
		PatientAlias  string           `json:"patient_alias"`
		OnsetDate     *string          `json:"onset_date"`
		Trajectory    []map[string]any `json:"trajectory"`
		Source        string           `json:"source"`
	}
	cases := []EmergCase{}
	crows, err := db.Query(`SELECT ec.id, COALESCE(cm.name,''), ec.case_status, ec.patient_alias, to_char(ec.onset_date,'YYYY-MM-DD'), ec.trajectory, ec.source
		FROM emergency_cases ec LEFT JOIN communities cm ON cm.id=ec.community_id WHERE ec.emergency_id=$1 ORDER BY ec.id`, id)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var x EmergCase
			var traj RawJSON
			crows.Scan(&x.ID, &x.CommunityName, &x.CaseStatus, &x.PatientAlias, &x.OnsetDate, &traj, &x.Source)
			x.StatusLabel = labelOf(EmergCaseStatusLabels, x.CaseStatus)
			if len(traj) > 0 {
				_ = json.Unmarshal(traj, &x.Trajectory)
			}
			if x.Trajectory == nil {
				x.Trajectory = []map[string]any{}
			}
			cases = append(cases, x)
		}
	}

	// 联防小区 + 各小区关联业务聚合
	type EmergCommunity struct {
		CommunityID        int64  `json:"community_id"`
		CommunityName      string `json:"community_name"`
		Role               string `json:"role"`
		RoleLabel          string `json:"role_label"`
		RiskLevel          string `json:"risk_level"`
		RiskLabel          string `json:"risk_label"`
		RiskReason         string `json:"risk_reason"`
		WorkOrderID        int64  `json:"work_order_id"`
		ComplaintsAtStart  int    `json:"complaints_at_start"`
		ComplaintsNow      int    `json:"complaints_now"`
		ComplaintDelta     int    `json:"complaint_delta"`
		OpenWaterPoints    int    `json:"open_water_points"`
		ClearedWaterPoints int    `json:"cleared_water_points"`
		Treatments         int    `json:"treatments"`
		RecheckLarvae      int    `json:"recheck_larvae"`
		RectOpen           int    `json:"rect_open"`
		RectOverdue        int    `json:"rect_overdue"`
		ChildZonePlans     int    `json:"child_zone_plans"`
		PetComplaints      int    `json:"pet_complaints"`
		ChemicalUsed       float64 `json:"chemical_used"`
	}
	comms := []EmergCommunity{}
	ecrows, err := db.Query(`SELECT ec.community_id, cm.name, ec.role, ec.risk_level, ec.risk_reason, ec.work_order_id, ec.complaints_at_start
		FROM emergency_communities ec JOIN communities cm ON cm.id=ec.community_id WHERE ec.emergency_id=$1 ORDER BY ec.role DESC, ec.id`, id)
	if err == nil {
		defer ecrows.Close()
		for ecrows.Next() {
			var x EmergCommunity
			ecrows.Scan(&x.CommunityID, &x.CommunityName, &x.Role, &x.RiskLevel, &x.RiskReason, &x.WorkOrderID, &x.ComplaintsAtStart)
			x.RoleLabel = labelOf(EmergCommunityRoleLabels, x.Role)
			x.RiskLabel = labelOf(CommunityRiskLabels, x.RiskLevel)
			cid := x.CommunityID
			since := e.StartedAt
			db.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1`, cid).Scan(&x.ComplaintsNow)
			x.ComplaintDelta = x.ComplaintsNow - x.ComplaintsAtStart
			db.QueryRow(`SELECT count(*) FROM water_points WHERE community_id=$1 AND status!='cleared'`, cid).Scan(&x.OpenWaterPoints)
			db.QueryRow(`SELECT count(*) FROM water_points WHERE community_id=$1 AND status='cleared' AND last_recheck_at >= $2`, cid, since).Scan(&x.ClearedWaterPoints)
			db.QueryRow(`SELECT count(*) FROM treatments t JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=$1 AND t.created_at >= $2`, cid, since).Scan(&x.Treatments)
			db.QueryRow(`SELECT COALESCE(sum(t.chemical_used),0) FROM treatments t JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=$1 AND t.created_at >= $2`, cid, since).Scan(&x.ChemicalUsed)
			db.QueryRow(`SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.community_id=$1 AND rc.result='larvae_found' AND rc.created_at >= $2`, cid, since).Scan(&x.RecheckLarvae)
			db.QueryRow(`SELECT count(*) FROM property_rectifications WHERE community_id=$1 AND status!='verified'`, cid).Scan(&x.RectOpen)
			db.QueryRow(`SELECT count(*) FROM property_rectifications WHERE community_id=$1 AND status!='verified' AND recheck_date < current_date`, cid).Scan(&x.RectOverdue)
			db.QueryRow(`SELECT count(*) FROM child_zone_plans WHERE community_id=$1 AND created_at >= $2`, cid, since).Scan(&x.ChildZonePlans)
			db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND created_at >= $2`, cid, since).Scan(&x.PetComplaints)
			comms = append(comms, x)
		}
	}

	// 重点积水点快照（含当前状态）
	type EmergWP struct {
		ID             int64  `json:"id"`
		CommunityName  string `json:"community_name"`
		WpType         string `json:"wp_type"`
		TypeLabel      string `json:"type_label"`
		Location       string `json:"location_desc"`
		LinkReason     string `json:"link_reason"`
		StatusSnapshot string `json:"status_snapshot"`
		CurrentStatus  string `json:"current_status"`
		StatusLabel    string `json:"status_label"`
		IsKey          bool   `json:"is_key"`
	}
	wps := []EmergWP{}
	wrows, err := db.Query(`SELECT ew.id, cm.name, ew.wp_type, ew.location_desc, ew.link_reason, ew.status_snapshot, ew.is_key,
		COALESCE(w.status,'') FROM emergency_water_points ew
		JOIN communities cm ON cm.id=ew.community_id LEFT JOIN water_points w ON w.id=ew.water_point_id
		WHERE ew.emergency_id=$1 ORDER BY ew.community_id, ew.is_key DESC, ew.id`, id)
	if err == nil {
		defer wrows.Close()
		for wrows.Next() {
			var x EmergWP
			wrows.Scan(&x.ID, &x.CommunityName, &x.WpType, &x.Location, &x.LinkReason, &x.StatusSnapshot, &x.IsKey, &x.CurrentStatus)
			x.TypeLabel = labelOf(WaterPointTypes, x.WpType)
			x.StatusLabel = labelOf(WaterPointStatusLabels, x.CurrentStatus)
			wps = append(wps, x)
		}
	}

	// 跨小区调度
	type Dispatch struct {
		ID           int64     `json:"id"`
		FromName     string    `json:"from_community_name"`
		ToName       string    `json:"to_community_name"`
		TeamName     string    `json:"team_name"`
		ResourceType string    `json:"resource_type"`
		ResourceLabel string   `json:"resource_label"`
		ResourceRef  string    `json:"resource_ref"`
		Amount       float64   `json:"amount"`
		Action       string    `json:"action"`
		CreatedAt    time.Time `json:"created_at"`
	}
	dispatches := []Dispatch{}
	drows, err := db.Query(`SELECT d.id, COALESCE(cf.name,''), ct.name, COALESCE(t.name,''), d.resource_type, d.resource_ref, d.amount, d.action, d.created_at
		FROM emergency_dispatch d
		JOIN communities ct ON ct.id=d.to_community_id
		LEFT JOIN communities cf ON cf.id=d.from_community_id
		LEFT JOIN teams t ON t.id=d.team_id
		WHERE d.emergency_id=$1 ORDER BY d.id`, id)
	if err == nil {
		defer drows.Close()
		for drows.Next() {
			var x Dispatch
			drows.Scan(&x.ID, &x.FromName, &x.ToName, &x.TeamName, &x.ResourceType, &x.ResourceRef, &x.Amount, &x.Action, &x.CreatedAt)
			x.ResourceLabel = labelOf(EmergResourceLabels, x.ResourceType)
			dispatches = append(dispatches, x)
		}
	}

	// 每日汇总
	type Daily struct {
		CommunityName string  `json:"community_name"`
		SummaryDate   string  `json:"summary_date"`
		NewComplaints int     `json:"new_complaints"`
		OpenWP        int     `json:"open_water_points"`
		ClearedWP     int     `json:"cleared_water_points"`
		ChemicalUsed  float64 `json:"chemical_used"`
		Treatments    int     `json:"treatments"`
		RectOpen      int     `json:"rect_open"`
		RectOverdue   int     `json:"rect_overdue"`
		Note          string  `json:"note"`
	}
	dailies := []Daily{}
	dyrows, err := db.Query(`SELECT s.summary_date, COALESCE(cm.name,'全响应合计'), s.new_complaints, s.open_water_points, s.cleared_water_points,
		s.chemical_used, s.treatments, s.rect_open, s.rect_overdue, s.note
		FROM emergency_daily_summaries s LEFT JOIN communities cm ON cm.id=s.community_id
		WHERE s.emergency_id=$1 ORDER BY s.summary_date DESC, s.community_id`, id)
	if err == nil {
		defer dyrows.Close()
		for dyrows.Next() {
			var x Daily
			dyrows.Scan(&x.SummaryDate, &x.CommunityName, &x.NewComplaints, &x.OpenWP, &x.ClearedWP,
				&x.ChemicalUsed, &x.Treatments, &x.RectOpen, &x.RectOverdue, &x.Note)
			dailies = append(dailies, x)
		}
	}

	jsonOK(c.W, map[string]any{
		"emergency":    e,
		"cases":        cases,
		"communities":  comms,
		"water_points": wps,
		"dispatches":   dispatches,
		"daily":        dailies,
	})
}

// ---------- 跨小区联防调度 ----------

func hEmergencyDispatch(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		FromCommunityID int64   `json:"from_community_id"`
		ToCommunityID   int64   `json:"to_community_id"`
		TeamID          int64   `json:"team_id"`
		ResourceType    string  `json:"resource_type"`
		ResourceRef     string  `json:"resource_ref"`
		ChemicalID      int64   `json:"chemical_id"`
		Amount          float64 `json:"amount"`
		Action          string  `json:"action"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM emergency_responses WHERE id=$1`, id).Scan(&status); err != nil {
		jsonErr(c.W, 404, "应急响应不存在")
		return
	}
	if status != "active" {
		jsonErr(c.W, 409, "应急响应已解除，不能再调度")
		return
	}
	if _, ok := EmergResourceLabels[req.ResourceType]; !ok {
		jsonErr(c.W, 400, "调度资源类型非法")
		return
	}
	var toName string
	if err := db.QueryRow(`SELECT name FROM communities WHERE id=$1`, req.ToCommunityID).Scan(&toName); err != nil {
		jsonErr(c.W, 400, "受援小区不存在")
		return
	}
	var toOrder int64
	if err := db.QueryRow(`SELECT work_order_id FROM emergency_communities WHERE emergency_id=$1 AND community_id=$2`, id, req.ToCommunityID).Scan(&toOrder); err != nil {
		jsonErr(c.W, 400, "受援小区不在本次联防范围")
		return
	}

	var teamID sql.NullInt64
	fromName := ""
	if req.FromCommunityID != 0 {
		db.QueryRow(`SELECT name FROM communities WHERE id=$1`, req.FromCommunityID).Scan(&fromName)
	}
	if req.ResourceType == "team" {
		if req.TeamID == 0 {
			jsonErr(c.W, 400, "跨小区支援请选择消杀队")
			return
		}
		teamID = sql.NullInt64{Int64: req.TeamID, Valid: true}
		var tname string
		db.QueryRow(`SELECT name FROM teams WHERE id=$1`, req.TeamID).Scan(&tname)
		// 支援队加入受援小区应急工单
		addTeamParties(toOrder, req.TeamID)
		db.Exec(`UPDATE work_orders SET team_id=COALESCE(team_id,$1), updated_at=now() WHERE id=$2 AND team_id IS NULL`, req.TeamID, toOrder)
		addLog(toOrder, c.User, "跨小区支援",
			fmt.Sprintf("调度「%s」%s → %s 支援应急消杀。%s", tname, nonDash(fromName), toName, req.Action))
	} else if req.ResourceType == "chemical" {
		if req.ChemicalID == 0 || req.Amount <= 0 {
			jsonErr(c.W, 400, "药剂调配请选择药剂与数量")
			return
		}
		var stock float64
		var cname, unit string
		if err := db.QueryRow(`SELECT stock, name, unit FROM chemicals WHERE id=$1 FOR UPDATE`, req.ChemicalID).Scan(&stock, &cname, &unit); err != nil {
			jsonErr(c.W, 400, "药剂不存在")
			return
		}
		if stock < req.Amount {
			jsonErr(c.W, 409, fmt.Sprintf("%s 库存仅 %.1f%s，调配数量不足，已提示药剂不足异常", cname, stock, unit))
			return
		}
		db.Exec(`UPDATE chemicals SET stock=stock-$1, updated_at=now() WHERE id=$2`, req.Amount, req.ChemicalID)
		req.ResourceRef = cname
		addLog(toOrder, c.User, "跨小区药剂调配",
			fmt.Sprintf("%s%s 向 %s 调配 %s %.1f%s。%s", nonDash(fromName), "应急储备", toName, cname, req.Amount, unit, req.Action))
	} else {
		// 网格/物业/卫生监督力量
		if req.ResourceRef == "" {
			req.ResourceRef = labelOf(EmergResourceLabels, req.ResourceType)
		}
		if req.ResourceType == "supervisor" {
			var sid int64
			if db.QueryRow(`SELECT id FROM users WHERE role='supervisor' ORDER BY id LIMIT 1`).Scan(&sid) == nil {
				ensureParty(toOrder, "supervisor", &sid, "应急卫生监督")
			}
		}
		if req.ResourceType == "property" {
			addCommunityPropertyParties(toOrder, req.ToCommunityID)
		}
		addLog(toOrder, c.User, "应急力量调度",
			fmt.Sprintf("协调%s（%s）支援 %s。%s", labelOf(EmergResourceLabels, req.ResourceType), req.ResourceRef, toName, req.Action))
	}

	db.Exec(`INSERT INTO emergency_dispatch(emergency_id, from_community_id, to_community_id, team_id, resource_type, resource_ref, amount, action, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, nullableID(req.FromCommunityID), req.ToCommunityID, teamID, req.ResourceType, req.ResourceRef, req.Amount, req.Action, c.User.ID)
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "联防调度已记录并同步到受援小区应急工单"})
}

func nonDash(s string) string {
	if s == "" {
		return ""
	}
	return s
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// ---------- 每日汇总 ----------

type emergDailyTotals struct {
	Complaints, Open, Cleared, Treatments, RectOpen, RectOverdue int
	Chem                                                         float64
}

func computeEmergencyDaily(cid int64, dayStart time.Time) emergDailyTotals {
	var t emergDailyTotals
	db.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1 AND created_at >= $2 AND created_at < $2 + interval '1 day'`, cid, dayStart).Scan(&t.Complaints)
	db.QueryRow(`SELECT count(*) FROM water_points WHERE community_id=$1 AND status!='cleared'`, cid).Scan(&t.Open)
	db.QueryRow(`SELECT count(*) FROM water_points WHERE community_id=$1 AND status='cleared' AND last_recheck_at >= $2`, cid, dayStart).Scan(&t.Cleared)
	db.QueryRow(`SELECT count(*) FROM treatments tr JOIN work_orders o ON o.id=tr.work_order_id WHERE o.community_id=$1 AND tr.created_at >= $2 AND tr.created_at < $2 + interval '1 day'`, cid, dayStart).Scan(&t.Treatments)
	db.QueryRow(`SELECT COALESCE(sum(tr.chemical_used),0) FROM treatments tr JOIN work_orders o ON o.id=tr.work_order_id WHERE o.community_id=$1 AND tr.created_at >= $2 AND tr.created_at < $2 + interval '1 day'`, cid, dayStart).Scan(&t.Chem)
	db.QueryRow(`SELECT count(*) FROM property_rectifications WHERE community_id=$1 AND status!='verified'`, cid).Scan(&t.RectOpen)
	db.QueryRow(`SELECT count(*) FROM property_rectifications WHERE community_id=$1 AND status!='verified' AND recheck_date < current_date`, cid).Scan(&t.RectOverdue)
	return t
}

func upsertEmergencyDaily(eid int64, cid any, day string, t emergDailyTotals, note string) {
	// community_id 可空（全响应合计），唯一约束对 NULL 不去重，故先按同键删除再插入保证幂等
	db.Exec(`DELETE FROM emergency_daily_summaries WHERE emergency_id=$1 AND community_id IS NOT DISTINCT FROM $2 AND summary_date=$3`, eid, cid, day)
	db.Exec(`INSERT INTO emergency_daily_summaries(emergency_id, community_id, summary_date, new_complaints, open_water_points, cleared_water_points, chemical_used, treatments, rect_open, rect_overdue, note)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		eid, cid, day, t.Complaints, t.Open, t.Cleared, t.Chem, t.Treatments, t.RectOpen, t.RectOverdue, note)
}

func hEmergencyDailySummary(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var started time.Time
	var status string
	if err := db.QueryRow(`SELECT started_at, status FROM emergency_responses WHERE id=$1`, id).Scan(&started, &status); err != nil {
		jsonErr(c.W, 404, "应急响应不存在")
		return
	}
	if status != "active" {
		jsonErr(c.W, 409, "应急响应已解除")
		return
	}
	day := time.Now().Format("2006-01-02")
	dayStart, _ := time.Parse("2006-01-02", day)

	var ecIDs []int64
	rows, _ := db.Query(`SELECT community_id FROM emergency_communities WHERE emergency_id=$1`, id)
	for rows.Next() {
		var cid int64
		rows.Scan(&cid)
		ecIDs = append(ecIDs, cid)
	}
	rows.Close()

	total := emergDailyTotals{}
	for _, cid := range ecIDs {
		t := computeEmergencyDaily(cid, dayStart)
		upsertEmergencyDaily(id, cid, day, t, "")
		total.Complaints += t.Complaints
		total.Open += t.Open
		total.Cleared += t.Cleared
		total.Treatments += t.Treatments
		total.RectOpen += t.RectOpen
		total.RectOverdue += t.RectOverdue
		total.Chem += t.Chem
	}
	upsertEmergencyDaily(id, nil, day, total, fmt.Sprintf("应急第 %d 天每日汇总", int(time.Since(started).Hours()/24)+1))
	jsonOK(c.W, map[string]any{
		"message": "每日汇总已生成（投诉变化/积水点清零/药剂消耗/整改责任）", "date": day,
		"new_complaints": total.Complaints, "open_water_points": total.Open, "cleared_water_points": total.Cleared,
		"chemical_used": total.Chem, "treatments": total.Treatments, "rect_open": total.RectOpen, "rect_overdue": total.RectOverdue,
	})
}

// ---------- 解除应急 → 自动降级 + 归档 ----------

func hResolveEmergency(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	decodeBody(c.R, &req)
	var status string
	if err := db.QueryRow(`SELECT status FROM emergency_responses WHERE id=$1`, id).Scan(&status); err != nil {
		jsonErr(c.W, 404, "应急响应不存在")
		return
	}
	if status != "active" {
		jsonErr(c.W, 409, "应急响应已解除")
		return
	}
	db.Exec(`UPDATE emergency_responses SET status='resolved', resolved_at=now(), resolve_note=$1 WHERE id=$2`, req.Note, id)

	// 收集本次联防小区
	var cids []int64
	rows, _ := db.Query(`SELECT community_id, work_order_id FROM emergency_communities WHERE emergency_id=$1`, id)
	type co struct{ cid, oid int64 }
	var pairs []co
	for rows.Next() {
		var x co
		rows.Scan(&x.cid, &x.oid)
		cids = append(cids, x.cid)
		pairs = append(pairs, x)
	}
	rows.Close()
	for _, x := range pairs {
		// 若小区无其他进行中的应急，则降级为常态
		var other int
		db.QueryRow(`SELECT count(*) FROM emergency_communities ec JOIN emergency_responses e ON e.id=ec.emergency_id
			WHERE ec.community_id=$1 AND e.status='active' AND e.id != $2`, x.cid, id).Scan(&other)
		if other == 0 {
			db.Exec(`UPDATE communities SET risk_level='normal', risk_reason='', risk_updated_at=now() WHERE id=$1`, x.cid)
		}
		// 关闭应急协同工单并记录
		db.Exec(`UPDATE work_orders SET status='closed', closed_at=now(), updated_at=now() WHERE id=$1 AND status!='closed'`, x.oid)
		addLog(x.oid, c.User, "解除应急响应", "风险解除，小区风险等级自动降级，应急处置归档到小区消杀档案。"+req.Note)
	}
	cacheDel("dashboard:closedloop", "emerg:active")
	jsonOK(c.W, map[string]any{"message": "应急响应已解除，联防小区自动降级，处置过程归入小区消杀档案供考核、计划与追溯"})
}

// ---------- 风险小区总览 ----------

func hRiskCommunities(c *Ctx) {
	rows, err := db.Query(`SELECT cm.id, cm.name, cm.risk_level, cm.risk_reason,
		(SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.created_at > now() - interval '72 hours'),
		(SELECT count(*) FROM water_points w WHERE w.community_id=cm.id AND w.status!='cleared'),
		(SELECT e.emerg_no FROM emergency_communities ec JOIN emergency_responses e ON e.id=ec.emergency_id
		 WHERE ec.community_id=cm.id AND e.status='active' ORDER BY e.id DESC LIMIT 1)
		FROM communities cm
		WHERE cm.risk_level != 'normal'
		   OR cm.id IN (SELECT community_id FROM emergency_communities ec JOIN emergency_responses e ON e.id=ec.emergency_id WHERE e.status='active')
		ORDER BY CASE cm.risk_level WHEN 'emergency' THEN 1 WHEN 'warning' THEN 2 WHEN 'elevated' THEN 3 ELSE 4 END, cm.id`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type R struct {
		ID             int64   `json:"id"`
		Name           string  `json:"community_name"`
		RiskLevel      string  `json:"risk_level"`
		RiskLabel      string  `json:"risk_label"`
		RiskReason     string  `json:"risk_reason"`
		Complaints72h  int     `json:"complaints_72h"`
		OpenWaterPoints int    `json:"open_water_points"`
		ActiveEmergNo  *string `json:"active_emerg_no"`
	}
	list := []R{}
	for rows.Next() {
		var x R
		rows.Scan(&x.ID, &x.Name, &x.RiskLevel, &x.RiskReason, &x.Complaints72h, &x.OpenWaterPoints, &x.ActiveEmergNo)
		x.RiskLabel = labelOf(CommunityRiskLabels, x.RiskLevel)
		list = append(list, x)
	}
	jsonOK(c.W, list)
}
