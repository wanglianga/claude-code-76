package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func registerAccessRoutes() {
	handle("POST /api/work-orders/{id}/access-cases", hCreateAccessCase, "operator", "grid", "street", "supervisor", "property")
	handle("GET /api/access-cases", hListAccessCases)
	handle("GET /api/access-cases/{id}", hGetAccessCase)
	handle("POST /api/access-cases/{id}/negotiate", hAccessNegotiate, "street", "grid", "property", "supervisor", "operator")
	handle("POST /api/access-cases/{id}/mandatory-assess", hAccessMandatoryAssess, "supervisor")
	handle("POST /api/access-cases/{id}/authorize", hAccessAuthorize, "resident")
	handle("POST /api/access-cases/{id}/external", hAccessExternalOnly, "street", "supervisor", "grid")
	handle("POST /api/access-cases/{id}/start", hAccessStart, "operator")
	handle("POST /api/access-cases/{id}/withdraw", hAccessWithdraw, "resident")
	handle("POST /api/access-cases/{id}/complete", hAccessComplete, "operator")
	handle("POST /api/access-cases/{id}/continue-risk", hAccessContinueRisk, "street", "supervisor")
}

// AccessCase 居民拒绝入户与入户授权案例
type AccessCase struct {
	ID               int64      `json:"id"`
	CaseNo           string     `json:"case_no"`
	WorkOrderID      int64      `json:"work_order_id"`
	CommunityID      int64      `json:"community_id"`
	CommunityName    string     `json:"community_name"`
	ResidentID       int64      `json:"resident_user_id"`
	ResidentName     string     `json:"resident_name"`
	Address          string     `json:"address"`
	Status           string     `json:"status"`
	StatusLabel      string     `json:"status_label"`
	RejectReasons    StringList `json:"reject_reasons"`
	RejectReasonLabels []string  `json:"reject_reason_labels"`
	RejectNote       string     `json:"reject_note"`
	SensitiveGroups  StringList `json:"sensitive_groups"`
	SensitiveLabels  []string   `json:"sensitive_labels"`
	PetsDesc         string     `json:"pets_desc"`
	AcceptableTimes  string     `json:"acceptable_times"`
	AcceptableChemicals string  `json:"acceptable_chemicals"`
	OutdoorAllowed   bool       `json:"outdoor_allowed"`
	OutdoorAreas     StringList `json:"outdoor_areas"`
	OutdoorAreaLabels []string  `json:"outdoor_area_labels"`
	RecordedBy       *int64     `json:"recorded_by"`
	RejectedAt       *time.Time `json:"rejected_at"`

	MandatoryRequired bool      `json:"mandatory_required"`
	AssessNote        string    `json:"assess_note"`
	AssessedAt        *time.Time `json:"assessed_at"`
	Witnesses         string    `json:"witnesses"`
	FinalOpinion      string    `json:"final_opinion"`

	AuthScope             string   `json:"auth_scope"`
	AuthChemicalName      string   `json:"auth_chemical_name"`
	AuthConcentration     string   `json:"auth_concentration"`
	AuthSafetyInterval    float64  `json:"auth_safety_interval_hours"`
	AuthItemCover         bool     `json:"auth_item_cover"`
	AuthPetAvoid          bool     `json:"auth_pet_avoid"`
	AuthVulnerableAvoid   bool     `json:"auth_vulnerable_avoid"`
	AuthCompanion         string   `json:"auth_companion"`
	AuthPhotoConsent      bool     `json:"auth_photo_consent"`
	AuthNoticeDelivered   bool     `json:"auth_notice_delivered"`
	AuthorizedAt          *time.Time `json:"authorized_at"`

	WithdrawReason string     `json:"withdraw_reason"`
	RescheduleDate *string    `json:"reschedule_date"`
	ResourcesKept  bool       `json:"resources_kept"`
	WithdrawnAt    *time.Time `json:"withdrawn_at"`

	ExternalRectificationID *int64 `json:"external_rectification_id"`
	ExternalNote            string `json:"external_note"`

	SprayArea            string     `json:"spray_area"`
	WarningSign          bool       `json:"warning_sign"`
	WarningRemovedAt     *time.Time `json:"warning_removed_at"`
	CompletionPhotos     StringList `json:"completion_photos"`
	ResidentConfirmed    bool       `json:"resident_confirmed"`
	PetAvoidDone         bool       `json:"pet_avoid_done"`
	ChildSafetyInterval  float64    `json:"child_safety_interval_hours"`
	CompletedAt          *time.Time `json:"completed_at"`

	RiskContinued      bool   `json:"risk_continued"`
	RiskNote           string `json:"risk_note"`
	RecheckIntervalDays int   `json:"recheck_interval_days"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`

	Versions []AccessVersion `json:"versions,omitempty"`
}

// AccessVersion 版本记录
type AccessVersion struct {
	VersionNo int            `json:"version_no"`
	Action    string         `json:"action"`
	ActorID   *int64         `json:"actor_id"`
	ActorName *string        `json:"actor_name"`
	ActorRole string         `json:"actor_role"`
	RoleLabel string         `json:"role_label"`
	Snapshot  RawJSON        `json:"snapshot"`
	Note      string         `json:"note"`
	CreatedAt time.Time      `json:"created_at"`
}

const accessSelect = `
	SELECT a.id, a.case_no, a.work_order_id, a.community_id, cm.name, a.resident_user_id, ur.name, a.address,
	       a.status, a.reject_reasons, a.reject_note, a.sensitive_groups, a.pets_desc, a.acceptable_times,
	       a.acceptable_chemicals, a.outdoor_allowed, a.outdoor_areas, a.recorded_by, a.rejected_at,
	       a.mandatory_required, a.assess_note, a.assessed_at, a.witnesses, a.final_opinion,
	       a.auth_scope, a.auth_chemical_name, a.auth_concentration, a.auth_safety_interval_hours,
	       a.auth_item_cover, a.auth_pet_avoid, a.auth_vulnerable_avoid, a.auth_companion, a.auth_photo_consent,
	       a.auth_notice_delivered, a.authorized_at, a.withdraw_reason, to_char(a.reschedule_date,'YYYY-MM-DD'),
	       a.resources_kept, a.withdrawn_at, a.external_rectification_id, a.external_note,
	       a.spray_area, a.warning_sign, a.warning_removed_at, a.completion_photos, a.resident_confirmed,
	       a.pet_avoid_done, a.child_safety_interval_hours, a.completed_at,
	       a.risk_continued, a.risk_note, a.recheck_interval_days, a.created_at, a.updated_at
	FROM access_cases a
	JOIN communities cm ON cm.id=a.community_id
	JOIN users ur ON ur.id=a.resident_user_id`

func scanAccessCase(row interface{ Scan(...any) error }) (*AccessCase, error) {
	var a AccessCase
	err := row.Scan(&a.ID, &a.CaseNo, &a.WorkOrderID, &a.CommunityID, &a.CommunityName, &a.ResidentID, &a.ResidentName, &a.Address,
		&a.Status, &a.RejectReasons, &a.RejectNote, &a.SensitiveGroups, &a.PetsDesc, &a.AcceptableTimes,
		&a.AcceptableChemicals, &a.OutdoorAllowed, &a.OutdoorAreas, &a.RecordedBy, &a.RejectedAt,
		&a.MandatoryRequired, &a.AssessNote, &a.AssessedAt, &a.Witnesses, &a.FinalOpinion,
		&a.AuthScope, &a.AuthChemicalName, &a.AuthConcentration, &a.AuthSafetyInterval,
		&a.AuthItemCover, &a.AuthPetAvoid, &a.AuthVulnerableAvoid, &a.AuthCompanion, &a.AuthPhotoConsent,
		&a.AuthNoticeDelivered, &a.AuthorizedAt, &a.WithdrawReason, &a.RescheduleDate,
		&a.ResourcesKept, &a.WithdrawnAt, &a.ExternalRectificationID, &a.ExternalNote,
		&a.SprayArea, &a.WarningSign, &a.WarningRemovedAt, &a.CompletionPhotos, &a.ResidentConfirmed,
		&a.PetAvoidDone, &a.ChildSafetyInterval, &a.CompletedAt,
		&a.RiskContinued, &a.RiskNote, &a.RecheckIntervalDays, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.StatusLabel = labelOf(AccessStatusLabels, a.Status)
	for _, r := range a.RejectReasons {
		a.RejectReasonLabels = append(a.RejectReasonLabels, labelOf(AccessRejectReasonLabels, r))
	}
	for _, g := range a.SensitiveGroups {
		a.SensitiveLabels = append(a.SensitiveLabels, labelOf(AccessSensitiveLabels, g))
	}
	for _, ar := range a.OutdoorAreas {
		a.OutdoorAreaLabels = append(a.OutdoorAreaLabels, labelOf(AccessOutdoorAreaLabels, ar))
	}
	return &a, nil
}

// addAccessVersion 写入一条版本记录（拒绝/沟通/评估/授权/变更/反悔/延期/外围/完成/风险延续）
func addAccessVersion(a *AccessCase, action string, actor *SessionUser, note string) {
	var n int
	db.QueryRow(`SELECT COALESCE(max(version_no),0) FROM access_case_versions WHERE case_id=$1`, a.ID).Scan(&n)
	snap, _ := json.Marshal(a)
	var actorID any
	role := "system"
	if actor != nil {
		actorID = actor.ID
		role = actor.Role
	}
	db.Exec(`INSERT INTO access_case_versions(case_id, version_no, action, actor_id, actor_role, snapshot, note)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, a.ID, n+1, action, actorID, role, RawJSON(snap), note)
}

func loadAccessVersions(a *AccessCase) {
	a.Versions = []AccessVersion{}
	rows, err := db.Query(`SELECT v.version_no, v.action, v.actor_id, u.name, v.actor_role, v.snapshot, v.note, v.created_at
		FROM access_case_versions v LEFT JOIN users u ON u.id=v.actor_id
		WHERE v.case_id=$1 ORDER BY v.version_no`, a.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v AccessVersion
		rows.Scan(&v.VersionNo, &v.Action, &v.ActorID, &v.ActorName, &v.ActorRole, &v.Snapshot, &v.Note, &v.CreatedAt)
		v.RoleLabel = labelOf(RoleLabels, v.ActorRole)
		a.Versions = append(a.Versions, v)
	}
}

// canAccessCase 读权限
func canAccessCase(u *SessionUser, a *AccessCase) bool {
	switch u.Role {
	case "street", "supervisor":
		return true
	case "resident":
		return u.ID == a.ResidentID
	case "property", "grid":
		return u.CommunityID != nil && *u.CommunityID == a.CommunityID
	case "operator":
		var tid sql.NullInt64
		db.QueryRow(`SELECT team_id FROM work_orders WHERE id=$1`, a.WorkOrderID).Scan(&tid)
		if tid.Valid && u.TeamID != nil && tid.Int64 == *u.TeamID {
			return true
		}
		var n int
		db.QueryRow(`SELECT count(*) FROM work_order_parties WHERE work_order_id=$1 AND party_role='operator' AND user_id=$2`, a.WorkOrderID, u.ID).Scan(&n)
		return n > 0
	}
	return false
}

// ---------- 登记拒绝入户（不强行派单，五方同单） ----------

func hCreateAccessCase(c *Ctx) {
	orderID, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		ResidentID          int64    `json:"resident_user_id"`
		Address             string   `json:"address"`
		RejectReasons       []string `json:"reject_reasons"`
		RejectNote          string   `json:"reject_note"`
		SensitiveGroups     []string `json:"sensitive_groups"`
		PetsDesc            string   `json:"pets_desc"`
		AcceptableTimes     string   `json:"acceptable_times"`
		AcceptableChemicals string   `json:"acceptable_chemicals"`
		OutdoorAllowed      bool     `json:"outdoor_allowed"`
		OutdoorAreas        []string `json:"outdoor_areas"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if len(req.RejectReasons) == 0 {
		jsonErr(c.W, 400, "请至少选择一项拒绝原因")
		return
	}
	for _, r := range req.RejectReasons {
		if _, ok := AccessRejectReasonLabels[r]; !ok {
			jsonErr(c.W, 400, "拒绝原因非法："+r)
			return
		}
	}
	var commID int64
	var reportID sql.NullInt64
	var orderStatus string
	if err := db.QueryRow(`SELECT community_id, report_id, status FROM work_orders WHERE id=$1`, orderID).Scan(&commID, &reportID, &orderStatus); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if orderStatus == "closed" {
		jsonErr(c.W, 409, "工单已闭环")
		return
	}
	residentID := req.ResidentID
	if residentID == 0 {
		if reportID.Valid {
			db.QueryRow(`SELECT reporter_id FROM reports WHERE id=$1`, reportID.Int64).Scan(&residentID)
		}
	}
	if residentID == 0 {
		jsonErr(c.W, 400, "无法确定住户，请指定居民账号")
		return
	}
	// 同一工单仅允许一个进行中的入户案例
	var dup int
	db.QueryRow(`SELECT count(*) FROM access_cases WHERE work_order_id=$1 AND status NOT IN ('completed','risk_continued','external_only')`, orderID).Scan(&dup)
	if dup > 0 {
		jsonErr(c.W, 409, "该工单已存在进行中的入户授权案例")
		return
	}

	outdoor := StringList(req.OutdoorAreas)
	if req.OutdoorAllowed && len(outdoor) == 0 {
		outdoor = StringList{"doorway", "staircase"}
	}
	var id int64
	err := db.QueryRow(`INSERT INTO access_cases
		(work_order_id, community_id, resident_user_id, address, status, reject_reasons, reject_note, sensitive_groups,
		 pets_desc, acceptable_times, acceptable_chemicals, outdoor_allowed, outdoor_areas, recorded_by, rejected_at)
		VALUES($1,$2,$3,$4,'refused',$5,$6,$7,$8,$9,$10,$11,$12,$13,now()) RETURNING id`,
		orderID, commID, residentID, req.Address, StringList(req.RejectReasons), req.RejectNote, StringList(req.SensitiveGroups),
		req.PetsDesc, req.AcceptableTimes, req.AcceptableChemicals, req.OutdoorAllowed, outdoor, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE access_cases SET case_no='AC'||LPAD(id::text,8,'0') WHERE id=$1`, id)

	// 不强行派单：以「居民拒绝入户」异常把五方拉到同一工单协同（不指定消杀队强制入户）
	createIssue(orderID, c.User, "resident_refused",
		fmt.Sprintf("住户拒绝入户（%s）。敏感人群：%s；可接受时段：%s；可接受药剂：%s；允许外围：%v；%s",
			labelsOf(AccessRejectReasonLabels, req.RejectReasons), labelsOf(AccessSensitiveLabels, req.SensitiveGroups),
			nonEmpty(req.AcceptableTimes, "未提供"), nonEmpty(req.AcceptableChemicals, "未提供"), req.OutdoorAllowed, req.RejectNote))
	// 五方协商需要消杀队在场：工单未派单时拉一支消杀队作为“入户协调”参与方（不写 team_id、不强行派单入户）
	var teamID sql.NullInt64
	db.QueryRow(`SELECT team_id FROM work_orders WHERE id=$1`, orderID).Scan(&teamID)
	if !teamID.Valid {
		var tid int64
		if db.QueryRow(`SELECT s.team_id FROM team_schedules s WHERE s.work_date=current_date ORDER BY (max_orders-(
			SELECT count(*) FROM work_orders WHERE team_id=s.team_id AND scheduled_date=current_date AND status!='closed')) DESC LIMIT 1`).Scan(&tid) != nil {
			db.QueryRow(`SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&tid)
		}
		if tid != 0 {
			addTeamParties(orderID, tid)
		}
	}

	a, _ := scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, id))
	addLog(orderID, c.User, "居民拒绝入户",
		fmt.Sprintf("登记入户授权案例 %s：不强行派单，居民/物业/消杀队/街道/卫生监督同单协商。可接受时段「%s」，外围处理「%v」",
			a.CaseNo, nonEmpty(req.AcceptableTimes, "未提供"), req.OutdoorAllowed))
	addAccessVersion(a, "登记拒绝入户", c.User, req.RejectNote)
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]any{"id": id, "case_no": a.CaseNo, "message": "已登记拒绝入户，五方进入同一工单协商，暂不强行派单入户"})
}

func labelsOf(m map[string]string, keys []string) string {
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += "、"
		}
		s += labelOf(m, k)
	}
	return s
}

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---------- 列表 / 详情 ----------

func hListAccessCases(c *Ctx) {
	q := accessSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	switch c.User.Role {
	case "resident":
		q += ` AND a.resident_user_id=` + next()
		args = append(args, c.User.ID)
	case "property", "grid":
		if c.User.CommunityID != nil {
			q += ` AND a.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "operator":
		if c.User.TeamID != nil {
			q += ` AND (EXISTS (SELECT 1 FROM work_orders o WHERE o.id=a.work_order_id AND o.team_id=` + next() + `)
				OR EXISTS (SELECT 1 FROM work_order_parties p WHERE p.work_order_id=a.work_order_id AND p.party_role='operator' AND p.user_id=` + next() + `))`
			args = append(args, *c.User.TeamID, c.User.ID)
		} else {
			q += ` AND false`
		}
	case "kindergarten":
		q += ` AND false`
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND a.status=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY (a.status IN ('completed','risk_continued','external_only')), a.id DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []AccessCase{}
	for rows.Next() {
		a, err := scanAccessCase(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *a)
	}
	jsonOK(c.W, list)
}

func hGetAccessCase(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	a, err := scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "入户授权案例不存在")
		return
	}
	if !canAccessCase(c.User, a) {
		jsonErr(c.W, 403, "无权查看该入户授权案例")
		return
	}
	loadAccessVersions(a)
	jsonOK(c.W, a)
}

// ---------- 多方上门沟通 ----------

func hAccessNegotiate(c *Ctx) {
	a, ok := loadAccessForAction(c, "negotiate", false)
	if !ok {
		return
	}
	switch a.Status {
	case "refused", "negotiating", "mandatory_review":
	default:
		jsonErr(c.W, 409, "当前状态（"+a.StatusLabel+"）无需再上门沟通")
		return
	}
	var req struct {
		Note         string `json:"note"`
		Witnesses    string `json:"witnesses"`
		FinalOpinion string `json:"final_opinion"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Note == "" {
		jsonErr(c.W, 400, "请填写上门沟通结果")
		return
	}
	db.Exec(`UPDATE access_cases SET status='negotiating', witnesses=COALESCE(NULLIF($1,''),witnesses),
		final_opinion=CASE WHEN $2='' THEN final_opinion ELSE $2 END, updated_at=now() WHERE id=$3`,
		req.Witnesses, req.FinalOpinion, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "入户沟通",
		fmt.Sprintf("%s。见证人：%s；居民最终意见：%s", req.Note, nonEmpty(req.Witnesses, "无"), nonEmpty(req.FinalOpinion, "待补充")))
	addAccessVersion(a, "上门沟通记录", c.User, req.Note)
	jsonOK(c.W, map[string]string{"message": "沟通结果、见证人与居民意见已记录"})
}

// ---------- 卫监强制入户评估（重点风险期 / 周边疑似病例） ----------

func hAccessMandatoryAssess(c *Ctx) {
	a, ok := loadAccessForAction(c, "mandatory", false)
	if !ok {
		return
	}
	switch a.Status {
	case "refused", "negotiating", "mandatory_review":
	default:
		jsonErr(c.W, 409, "仅尚未授权的案例可做入户必要性评估")
		return
	}
	var req struct {
		Required bool   `json:"required"`
		Note     string `json:"note"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Note == "" {
		jsonErr(c.W, 400, "请填写卫生监督评估意见")
		return
	}
	riskCtx := ""
	if rp := activeRiskPeriod(); rp != nil {
		riskCtx = fmt.Sprintf("当前处于登革热等重点风险期「%s」（复查间隔 %d 天）；", rp.Name, rp.RecheckIntervalDays)
	}
	if iv, inEm := emergencyRecheckInterval(a.CommunityID); inEm {
		riskCtx += fmt.Sprintf("本小区处于应急联防（复查间隔 %d 天）；", iv)
	}
	if req.Required {
		db.Exec(`UPDATE access_cases SET status='mandatory_review', mandatory_required=true, assess_note=$1, assess_by=$2, assessed_at=now(), updated_at=now() WHERE id=$3`,
			riskCtx+req.Note, c.User.ID, a.ID)
		addLog(a.WorkOrderID, c.User, "卫监评估必须入户", riskCtx+req.Note+" —— 街道、社区网格、物业继续上门沟通，未授权不得入户。")
	} else {
		db.Exec(`UPDATE access_cases SET mandatory_required=false, assess_note=$1, assess_by=$2, assessed_at=now(), updated_at=now() WHERE id=$1`,
			riskCtx+req.Note, c.User.ID, a.ID)
		addLog(a.WorkOrderID, c.User, "卫监评估可外围处理", riskCtx+req.Note)
	}
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addAccessVersion(a, "卫生监督入户必要性评估", c.User, req.Note)
	jsonOK(c.W, map[string]any{"message": map[bool]string{true: "评估为必须入户，继续多方上门沟通（未授权不得入户）", false: "评估为可仅做外围公共区域处理"}[req.Required], "risk_context": riskCtx})
}

// ---------- 居民授权确认 ----------

func hAccessAuthorize(c *Ctx) {
	a, ok := loadAccessForAction(c, "authorize", true)
	if !ok {
		return
	}
	switch a.Status {
	case "refused", "negotiating", "mandatory_review", "withdrawn":
	default:
		jsonErr(c.W, 409, "当前状态（"+a.StatusLabel+"）不能进行授权确认")
		return
	}
	var req struct {
		Scope                string   `json:"auth_scope"`
		ChemicalName         string   `json:"auth_chemical_name"`
		Concentration        string   `json:"auth_concentration"`
		SafetyIntervalHours  float64  `json:"auth_safety_interval_hours"`
		ItemCover            bool     `json:"auth_item_cover"`
		PetAvoid             bool     `json:"auth_pet_avoid"`
		VulnerableAvoid      bool     `json:"auth_vulnerable_avoid"`
		Companion            string   `json:"auth_companion"`
		PhotoConsent         bool     `json:"auth_photo_consent"`
		NoticeDelivered      bool     `json:"auth_notice_delivered"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.Scope == "" || req.ChemicalName == "" || req.SafetyIntervalHours <= 0 {
		jsonErr(c.W, 400, "请确认消杀范围、药剂名称与安全间隔")
		return
	}
	if !req.PhotoConsent || !req.NoticeDelivered {
		jsonErr(c.W, 400, "须确认拍照留证同意书与告知书已送达后方可授权")
		return
	}
	db.Exec(`UPDATE access_cases SET status='authorized', auth_scope=$1, auth_chemical_name=$2, auth_concentration=$3,
		auth_safety_interval_hours=$4, auth_item_cover=$5, auth_pet_avoid=$6, auth_vulnerable_avoid=$7,
		auth_companion=$8, auth_photo_consent=$9, auth_notice_delivered=$10, authorized_at=now(), updated_at=now() WHERE id=$11`,
		req.Scope, req.ChemicalName, req.Concentration, req.SafetyIntervalHours, req.ItemCover, req.PetAvoid,
		req.VulnerableAvoid, req.Companion, req.PhotoConsent, req.NoticeDelivered, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "居民授权入户",
		fmt.Sprintf("范围「%s」；药剂 %s（%s）；安全间隔 %.1f 小时；物品遮盖:%v 宠物避让:%v 老幼孕回避:%v；陪同人:%s；拍照留证:已同意；告知书:已送达",
			req.Scope, req.ChemicalName, req.Concentration, req.SafetyIntervalHours, req.ItemCover, req.PetAvoid, req.VulnerableAvoid, nonEmpty(req.Companion, "无")))
	addAccessVersion(a, "居民授权确认", c.User, "授权范围/药剂/安全间隔/遮盖/避让/陪同/拍照/告知书")
	jsonOK(c.W, map[string]string{"message": "已授权，消杀队可在约定时段入户作业"})
}

// ---------- 仅外围公共区域处理（关联物业责任与复查） ----------

func hAccessExternalOnly(c *Ctx) {
	a, ok := loadAccessForAction(c, "external", false)
	if !ok {
		return
	}
	switch a.Status {
	case "refused", "negotiating", "mandatory_review":
	default:
		jsonErr(c.W, 409, "仅尚未授权的案例可转为外围公共区域处理")
		return
	}
	var req struct {
		Note           string `json:"note"`
		PropertyUserID int64  `json:"property_user_id"`
		Deadline       string `json:"deadline"`
	}
	decodeBody(c.R, &req)
	if req.Note == "" {
		req.Note = "住户仅授权外围公共区域，户内积水无法根治，由物业处理门口/楼道/阳台外围/下水道并安排复查"
	}
	// 关联物业责任：复用通用整改 rectifications（公共区域责任）
	var propID int64
	if req.PropertyUserID != 0 {
		propID = req.PropertyUserID
	} else {
		db.QueryRow(`SELECT id FROM users WHERE role='property' AND community_id=$1 ORDER BY id LIMIT 1`, a.CommunityID).Scan(&propID)
	}
	var rectID any
	if propID != 0 {
		deadline := req.Deadline
		if deadline == "" {
			deadline = time.Now().AddDate(0, 0, 3).Format("2006-01-02")
		}
		var rid int64
		if db.QueryRow(`INSERT INTO rectifications(work_order_id, property_user_id, description, deadline)
			VALUES($1,$2,$3,$4) RETURNING id`, a.WorkOrderID, propID, "入户被拒·外围公共区域积水处理："+req.Note, deadline).Scan(&rid) == nil {
			rectID = rid
			ensureParty(a.WorkOrderID, "property", &propID, "外围区域整改责任物业")
		}
	}
	db.Exec(`UPDATE access_cases SET status='external_only', external_rectification_id=$1, external_note=$2, updated_at=now() WHERE id=$3`,
		rectID, req.Note, a.ID)
	createIssue(a.WorkOrderID, c.User, "property_facility", "居民仅授权外围，公共区域积水转物业处理并安排复查："+req.Note)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "转外围公共区域处理", req.Note)
	addAccessVersion(a, "仅外围处理（关联物业责任与复查）", c.User, req.Note)
	jsonOK(c.W, map[string]any{"message": "已转外围公共区域处理，关联物业责任与复查任务", "rectification_id": rectID})
}

// ---------- 入户开始（未授权不得入户） ----------

func hAccessStart(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	a, err := scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "入户授权案例不存在")
		return
	}
	if a.Status != "authorized" {
		jsonErr(c.W, 409, "未获得居民授权，不得入户")
		return
	}
	if !canAccessCase(c.User, a) {
		jsonErr(c.W, 403, "无权操作该入户授权案例")
		return
	}
	// 归属：仅本队消杀可开工
	var tid sql.NullInt64
	db.QueryRow(`SELECT team_id FROM work_orders WHERE id=$1`, a.WorkOrderID).Scan(&tid)
	if !tid.Valid || c.User.TeamID == nil || tid.Int64 != *c.User.TeamID {
		jsonErr(c.W, 403, "仅被指派的消杀队可入户作业")
		return
	}
	db.Exec(`UPDATE access_cases SET status='in_progress', updated_at=now() WHERE id=$1`, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "入户作业开始", "按授权范围与安全间隔入户作业")
	addAccessVersion(a, "入户作业开始", c.User, "")
	jsonOK(c.W, map[string]string{"message": "入户作业已开始"})
}

// ---------- 居民临时反悔：暂停，药剂与排班保留或改约 ----------

func hAccessWithdraw(c *Ctx) {
	a, ok := loadAccessForAction(c, "withdraw", true)
	if !ok {
		return
	}
	if a.Status != "authorized" && a.Status != "in_progress" {
		jsonErr(c.W, 409, "仅已授权/作业中可临时反悔（反悔后须重新授权）")
		return
	}
	var req struct {
		Reason         string `json:"reason"`
		RescheduleDate string `json:"reschedule_date"`
		ResourcesKept  *bool  `json:"resources_kept"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Reason == "" {
		jsonErr(c.W, 400, "请填写临时反悔原因")
		return
	}
	kept := true
	if req.ResourcesKept != nil {
		kept = *req.ResourcesKept
	}
	var rd any
	if req.RescheduleDate != "" {
		rd = req.RescheduleDate
	}
	db.Exec(`UPDATE access_cases SET status='withdrawn', withdraw_reason=$1, reschedule_date=$2, resources_kept=$3, withdrawn_at=now(), updated_at=now() WHERE id=$4`,
		req.Reason, rd, kept, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	// 作业暂停；药剂与排班保留（不扣减/不释放），或改约
	addLog(a.WorkOrderID, c.User, "居民临时反悔",
		fmt.Sprintf("入户作业暂停。原因：%s；%s；药剂与排班%s。", req.Reason, nonEmpty(req.RescheduleDate, "改约：待协商"),
			map[bool]string{true: "保留", false: "另行调度"}[kept]))
	addAccessVersion(a, "居民临时反悔（作业暂停/改约）", c.User, req.Reason)
	jsonOK(c.W, map[string]string{"message": "作业已暂停，已记录反悔原因，药剂与排班按约定保留或改约"})
}

// ---------- 入户作业完成 ----------

func hAccessComplete(c *Ctx) {
	a, ok := loadAccessForAction(c, "complete", false)
	if !ok {
		return
	}
	var req struct {
		SprayArea            string   `json:"spray_area"`
		WarningSign          bool     `json:"warning_sign"`
		WarningRemovedAt     string   `json:"warning_removed_at"`
		Photos               []string `json:"completion_photos"`
		ResidentConfirmed    bool     `json:"resident_confirmed"`
		PetAvoidDone         bool     `json:"pet_avoid_done"`
		ChildSafetyInterval  float64  `json:"child_safety_interval_hours"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.SprayArea == "" {
		jsonErr(c.W, 400, "请填写户内喷洒区域")
		return
	}
	if a.Status != "in_progress" {
		jsonErr(c.W, 409, "仅入户作业中的案例可登记完成")
		return
	}
	var removedAt any
	if req.WarningRemovedAt != "" {
		layouts := []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"}
		for _, lf := range layouts {
			if t, err := time.ParseInLocation(lf, req.WarningRemovedAt, time.Local); err == nil {
				removedAt = t
				break
			}
		}
	}
	interval := req.ChildSafetyInterval
	if interval <= 0 {
		interval = a.AuthSafetyInterval
	}
	db.Exec(`UPDATE access_cases SET status='completed', spray_area=$1, warning_sign=$2, warning_removed_at=$3,
		completion_photos=$4, resident_confirmed=$5, pet_avoid_done=$6, child_safety_interval_hours=$7,
		completed_by=$8, completed_at=now(), updated_at=now() WHERE id=$9`,
		req.SprayArea, req.WarningSign, removedAt, StringList(req.Photos), req.ResidentConfirmed, req.PetAvoidDone, interval, c.User.ID, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "入户作业完成",
		fmt.Sprintf("喷洒区域：%s；警示牌:%v；宠物避让:%v；儿童/敏感人群安全间隔 %.1f 小时；居民确认:%v。居民端可查看作业与撤除时间。",
			req.SprayArea, req.WarningSign, req.PetAvoidDone, interval, req.ResidentConfirmed))
	addAccessVersion(a, "入户作业完成", c.User, "喷洒区域/警示牌撤除/居民确认/宠物避让/儿童安全间隔")
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "入户作业已完成，居民端可查看喷洒区域、警示撤除时间与安全间隔"})
}

// ---------- 无法根治 → 风险延续，提高公共区域复查频次，进入考核/计划 ----------

func hAccessContinueRisk(c *Ctx) {
	a, ok := loadAccessForAction(c, "risk", false)
	if !ok {
		return
	}
	switch a.Status {
	case "refused", "negotiating", "mandatory_review", "external_only", "withdrawn":
	default:
		jsonErr(c.W, 409, "仅未完成入户根治的案例可标记风险延续")
		return
	}
	var req struct {
		Note               string `json:"note"`
		RecheckIntervalDays int   `json:"recheck_interval_days"`
	}
	decodeBody(c.R, &req)
	if req.Note == "" {
		req.Note = "因居民拒绝入户无法根治户内积水，风险延续，提高公共区域复查频次"
	}
	interval := req.RecheckIntervalDays
	if interval <= 0 {
		if iv, ok := emergencyRecheckInterval(a.CommunityID); ok {
			interval = iv
		} else if rp := activeRiskPeriod(); rp != nil {
			interval = rp.RecheckIntervalDays
		} else {
			interval = 3
		}
	}
	db.Exec(`UPDATE access_cases SET status='risk_continued', risk_continued=true, risk_note=$1, recheck_interval_days=$2, updated_at=now() WHERE id=$3`,
		req.Note, interval, a.ID)
	a, _ = scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, a.ID))
	addLog(a.WorkOrderID, c.User, "标记风险延续",
		fmt.Sprintf("%s；公共区域复查频次提高为每 %d 天一次，进入街道考核与下次消杀计划。", req.Note, interval))
	addAccessVersion(a, "风险延续（提频/考核/计划）", c.User, req.Note)
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]any{"message": "已标记风险延续，公共区域复查提频并纳入考核与下次计划", "recheck_interval_days": interval})
}

// loadAccessForAction 加载案例并做权限校验；residentOnly=true 时仅住户本人
func loadAccessForAction(c *Ctx, action string, residentOnly bool) (*AccessCase, bool) {
	id, ok := pathID(c, "id")
	if !ok {
		return nil, false
	}
	a, err := scanAccessCase(db.QueryRow(accessSelect+` WHERE a.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "入户授权案例不存在")
		return nil, false
	}
	if residentOnly {
		if c.User.Role != "resident" || c.User.ID != a.ResidentID {
			jsonErr(c.W, 403, "仅住户本人可执行此操作")
			return nil, false
		}
	} else if !canAccessCase(c.User, a) {
		jsonErr(c.W, 403, "无权操作该入户授权案例")
		return nil, false
	}
	return a, true
}
