package main

import (
	"fmt"
	"time"
)

func registerPetRoutes() {
	handle("POST /api/pet-complaints", hCreatePetComplaint, "resident")
	handle("GET /api/pet-complaints", hListPetComplaints)
	handle("GET /api/pet-complaints/tracking", hPetTracking, "street", "supervisor")
	handle("GET /api/pet-complaints/{id}", hGetPetComplaint)
	handle("POST /api/pet-complaints/{id}/vouchers", hAddPetVouchers, "resident")
	handle("POST /api/pet-complaints/{id}/handle", hHandlePetComplaint, "supervisor", "street")
	handle("GET /api/communities/{id}/archive", hCommunityArchive, "street", "supervisor")
}

// ---------- 宠物误触投诉 ----------

type PetComplaint struct {
	ID              int64      `json:"id"`
	ComplaintNo     string     `json:"complaint_no"`
	WorkOrderID     *int64     `json:"work_order_id"`
	OrderNo         *string    `json:"order_no"`
	TreatmentID     *int64     `json:"treatment_id"`
	CommunityID     int64      `json:"community_id"`
	CommunityName   string     `json:"community_name"`
	ReporterID      int64      `json:"reporter_user_id"`
	ReporterName    string     `json:"reporter_name"`
	PetType         string     `json:"pet_type"`
	PetTypeLabel    string     `json:"pet_type_label"`
	PetName         string     `json:"pet_name"`
	Symptom         string     `json:"symptom"`
	WalkingRoute    string     `json:"walking_route"`
	ChemicalID      *int64     `json:"chemical_id"`
	ChemicalName    string     `json:"chemical_name"`
	SprayArea       string     `json:"spray_area"`
	WarningTime     *time.Time `json:"warning_time"`
	MedicalVouchers StringList `json:"medical_vouchers"`
	Status          string     `json:"status"`
	StatusLabel     string     `json:"status_label"`
	NeedRevisit     bool       `json:"need_revisit"`
	Compensation    bool       `json:"compensation"`
	CompensationNote string    `json:"compensation_note"`
	ChemicalNote    string     `json:"chemical_note"`
	NotifyAdjustment string    `json:"notify_adjustment"`
	ResolutionNote  string     `json:"resolution_note"`
	HandledBy       *int64     `json:"handled_by"`
	HandlerName     *string    `json:"handler_name"`
	HandledAt       *time.Time `json:"handled_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

const petSelect = `
	SELECT p.id, p.complaint_no, p.work_order_id, o.order_no, p.treatment_id, p.community_id, cm.name,
	       p.reporter_user_id, u.name, p.pet_type, p.pet_name, p.symptom, p.walking_route,
	       p.chemical_id, p.chemical_name, p.spray_area, p.warning_time, p.medical_vouchers, p.status,
	       p.need_revisit, p.compensation, p.compensation_note, p.chemical_note, p.notify_adjustment,
	       p.resolution_note, p.handled_by, hu.name, p.handled_at, p.created_at
	FROM pet_complaints p
	JOIN communities cm ON cm.id=p.community_id
	JOIN users u ON u.id=p.reporter_user_id
	LEFT JOIN work_orders o ON o.id=p.work_order_id
	LEFT JOIN users hu ON hu.id=p.handled_by`

func scanPet(row interface{ Scan(...any) error }) (*PetComplaint, error) {
	var p PetComplaint
	err := row.Scan(&p.ID, &p.ComplaintNo, &p.WorkOrderID, &p.OrderNo, &p.TreatmentID, &p.CommunityID, &p.CommunityName,
		&p.ReporterID, &p.ReporterName, &p.PetType, &p.PetName, &p.Symptom, &p.WalkingRoute,
		&p.ChemicalID, &p.ChemicalName, &p.SprayArea, &p.WarningTime, &p.MedicalVouchers, &p.Status,
		&p.NeedRevisit, &p.Compensation, &p.CompensationNote, &p.ChemicalNote, &p.NotifyAdjustment,
		&p.ResolutionNote, &p.HandledBy, &p.HandlerName, &p.HandledAt, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.PetTypeLabel = labelOf(PetTypes, p.PetType)
	p.StatusLabel = labelOf(PetComplaintStatusLabels, p.Status)
	return &p, nil
}

// hCreatePetComplaint 宠物主人反馈喷药后宠物不适：关联药剂、喷洒区域、警示时间与行走路线
func hCreatePetComplaint(c *Ctx) {
	var req struct {
		WorkOrderID     *int64   `json:"work_order_id"`
		PetType         string   `json:"pet_type"`
		PetName         string   `json:"pet_name"`
		Symptom         string   `json:"symptom"`
		WalkingRoute    string   `json:"walking_route"`
		MedicalVouchers []string `json:"medical_vouchers"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if _, ok := PetTypes[req.PetType]; !ok {
		jsonErr(c.W, 400, "宠物类型非法")
		return
	}
	if req.Symptom == "" || req.WalkingRoute == "" {
		jsonErr(c.W, 400, "请填写宠物不适症状与主人行走路线")
		return
	}
	if c.User.CommunityID == nil {
		jsonErr(c.W, 400, "账号未关联小区")
		return
	}
	commID := *c.User.CommunityID

	// 关联消杀记录：指定工单取该单最近一次消杀；否则取本小区最近一次消杀
	var treatmentID, orderID *int64
	var chemID *int64
	var chemName, sprayArea string
	var warningTime *time.Time
	if req.WorkOrderID != nil {
		var commOfOrder int64
		if err := db.QueryRow(`SELECT community_id FROM work_orders WHERE id=$1`, *req.WorkOrderID).Scan(&commOfOrder); err != nil {
			jsonErr(c.W, 404, "关联工单不存在")
			return
		}
		if commOfOrder != commID {
			jsonErr(c.W, 403, "只能关联本小区的消杀工单")
			return
		}
		orderID = req.WorkOrderID
		db.QueryRow(`SELECT id, chemical_id, chemical_name, spray_area, treated_at FROM treatments
			WHERE work_order_id=$1 ORDER BY id DESC LIMIT 1`, *req.WorkOrderID).
			Scan(&treatmentID, &chemID, &chemName, &sprayArea, &warningTime)
	} else {
		var oid int64
		err := db.QueryRow(`SELECT t.id, t.work_order_id, t.chemical_id, t.chemical_name, t.spray_area, t.treated_at
			FROM treatments t JOIN work_orders o ON o.id=t.work_order_id
			WHERE o.community_id=$1 ORDER BY t.id DESC LIMIT 1`, commID).
			Scan(&treatmentID, &oid, &chemID, &chemName, &sprayArea, &warningTime)
		if err == nil {
			orderID = &oid
		}
	}

	var id int64
	err := db.QueryRow(`INSERT INTO pet_complaints(work_order_id, treatment_id, community_id, reporter_user_id,
		pet_type, pet_name, symptom, walking_route, chemical_id, chemical_name, spray_area, warning_time, medical_vouchers)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		orderID, treatmentID, commID, c.User.ID, req.PetType, req.PetName, req.Symptom, req.WalkingRoute,
		chemID, chemName, sprayArea, warningTime, StringList(req.MedicalVouchers)).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE pet_complaints SET complaint_no='PC'||LPAD(id::text,6,'0') WHERE id=$1`, id)
	if orderID != nil {
		addLog(*orderID, c.User, "宠物误触投诉", fmt.Sprintf("宠物%s「%s」喷药后不适：%s；行走路线：%s",
			labelOf(PetTypes, req.PetType), req.PetName, req.Symptom, req.WalkingRoute))
		// 工单未闭环时同步生成「宠物主人投诉」异常，进入五方协同
		var st string
		if db.QueryRow(`SELECT status FROM work_orders WHERE id=$1`, *orderID).Scan(&st) == nil && st != "closed" {
			createIssue(*orderID, c.User, "pet_complaint",
				fmt.Sprintf("宠物%s「%s」喷药后不适：%s（详见宠物投诉 PC 单）", labelOf(PetTypes, req.PetType), req.PetName, req.Symptom))
		}
	}
	p, err := scanPet(db.QueryRow(petSelect+` WHERE p.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, p)
}

func hListPetComplaints(c *Ctx) {
	q := petSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	switch c.User.Role {
	case "resident":
		q += ` AND p.reporter_user_id=` + next()
		args = append(args, c.User.ID)
	case "property", "grid":
		if c.User.CommunityID != nil {
			q += ` AND p.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "operator":
		if c.User.TeamID != nil {
			q += ` AND p.work_order_id IN (SELECT id FROM work_orders WHERE team_id=` + next() + `)`
			args = append(args, *c.User.TeamID)
		}
	case "kindergarten":
		jsonOK(c.W, []PetComplaint{})
		return
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND p.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND p.community_id=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY p.id DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []PetComplaint{}
	for rows.Next() {
		p, err := scanPet(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *p)
	}
	jsonOK(c.W, list)
}

func hGetPetComplaint(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, err := scanPet(db.QueryRow(petSelect+` WHERE p.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "投诉不存在")
		return
	}
	u := c.User
	switch u.Role {
	case "resident":
		if p.ReporterID != u.ID {
			jsonErr(c.W, 404, "投诉不存在")
			return
		}
	case "property", "grid":
		if u.CommunityID == nil || *u.CommunityID != p.CommunityID {
			jsonErr(c.W, 403, "仅可查看本小区投诉")
			return
		}
	}
	jsonOK(c.W, p)
}

// hAddPetVouchers 宠物主人补充就医凭证
func hAddPetVouchers(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Vouchers []string `json:"vouchers"`
	}
	if err := decodeBody(c.R, &req); err != nil || len(req.Vouchers) == 0 {
		jsonErr(c.W, 400, "请提供就医凭证链接")
		return
	}
	var ownerID int64
	var existing StringList
	if err := db.QueryRow(`SELECT reporter_user_id, medical_vouchers FROM pet_complaints WHERE id=$1`, id).Scan(&ownerID, &existing); err != nil {
		jsonErr(c.W, 404, "投诉不存在")
		return
	}
	if ownerID != c.User.ID {
		jsonErr(c.W, 403, "仅投诉人本人可补充凭证")
		return
	}
	merged := append(existing, req.Vouchers...)
	if _, err := db.Exec(`UPDATE pet_complaints SET medical_vouchers=$1 WHERE id=$2`, StringList(merged), id); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, map[string]string{"message": "就医凭证已补充"})
}

// hHandlePetComplaint 卫生监督/街道办结：回访、赔付、药剂说明、后续告知调整
func hHandlePetComplaint(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		NeedRevisit      bool   `json:"need_revisit"`
		Compensation     bool   `json:"compensation"`
		CompensationNote string `json:"compensation_note"`
		ChemicalNote     string `json:"chemical_note"`
		NotifyAdjustment string `json:"notify_adjustment"`
		ResolutionNote   string `json:"resolution_note"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	res, err := db.Exec(`UPDATE pet_complaints SET status='resolved', need_revisit=$1, compensation=$2,
		compensation_note=$3, chemical_note=$4, notify_adjustment=$5, resolution_note=$6, handled_by=$7, handled_at=now()
		WHERE id=$8 AND status='pending'`,
		req.NeedRevisit, req.Compensation, req.CompensationNote, req.ChemicalNote, req.NotifyAdjustment,
		req.ResolutionNote, c.User.ID, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "投诉不存在或已办结")
		return
	}
	var orderID *int64
	var petName string
	db.QueryRow(`SELECT work_order_id, pet_name FROM pet_complaints WHERE id=$1`, id).Scan(&orderID, &petName)
	if orderID != nil {
		addLog(*orderID, c.User, "宠物投诉办结", fmt.Sprintf("宠物「%s」：回访=%v 赔付=%v；药剂说明：%s；后续告知调整：%s；%s",
			petName, req.NeedRevisit, req.Compensation, req.ChemicalNote, req.NotifyAdjustment, req.ResolutionNote))
	}
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "投诉已办结，处理结果与告知调整已记录"})
}

// ---------- 同类投诉跟踪（告知调整后是否下降） ----------

type PetTracking struct {
	CommunityID       int64      `json:"community_id"`
	CommunityName     string     `json:"community_name"`
	TotalComplaints   int        `json:"total_complaints"`
	PendingComplaints int        `json:"pending_complaints"`
	LatestAdjustment  string     `json:"latest_adjustment"`
	AdjustedAt        *time.Time `json:"adjusted_at"`
	BeforeCount       int        `json:"before_count"`        // 调整前 30 天同类投诉
	AfterCount        int        `json:"after_count"`         // 调整后至今同类投诉
	BeforePerDay      float64    `json:"before_per_day"`
	AfterPerDay       float64    `json:"after_per_day"`
	Decreased         *bool      `json:"decreased"`           // 调整后是否下降（无调整为 null）
}

func petTrackingForCommunity(communityID int64) *PetTracking {
	t := &PetTracking{CommunityID: communityID}
	db.QueryRow(`SELECT name FROM communities WHERE id=$1`, communityID).Scan(&t.CommunityName)
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1`, communityID).Scan(&t.TotalComplaints)
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND status='pending'`, communityID).Scan(&t.PendingComplaints)
	var adjAt *time.Time
	db.QueryRow(`SELECT notify_adjustment, handled_at FROM pet_complaints
		WHERE community_id=$1 AND status='resolved' AND notify_adjustment != ''
		ORDER BY handled_at DESC LIMIT 1`, communityID).Scan(&t.LatestAdjustment, &adjAt)
	t.AdjustedAt = adjAt
	if adjAt != nil {
		db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND created_at >= $2 - interval '30 days' AND created_at < $2`, communityID, *adjAt).Scan(&t.BeforeCount)
		db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND created_at >= $2`, communityID, *adjAt).Scan(&t.AfterCount)
		daysAfter := time.Since(*adjAt).Hours() / 24
		if daysAfter < 1 {
			daysAfter = 1
		}
		t.BeforePerDay = float64(t.BeforeCount) / 30.0
		t.AfterPerDay = float64(t.AfterCount) / daysAfter
		decreased := t.AfterPerDay < t.BeforePerDay
		t.Decreased = &decreased
	}
	return t
}

func hPetTracking(c *Ctx) {
	commID := c.R.URL.Query().Get("community_id")
	if commID == "" {
		// 全部小区概览
		rows, err := db.Query(`SELECT id FROM communities ORDER BY id`)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		defer rows.Close()
		list := []*PetTracking{}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			list = append(list, petTrackingForCommunity(id))
		}
		jsonOK(c.W, list)
		return
	}
	var id int64
	if _, err := fmt.Sscanf(commID, "%d", &id); err != nil {
		jsonErr(c.W, 400, "community_id 非法")
		return
	}
	jsonOK(c.W, petTrackingForCommunity(id))
}

// ---------- 小区消杀档案 ----------

func hCommunityArchive(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM communities WHERE id=$1`, id).Scan(&name); err != nil {
		jsonErr(c.W, 404, "小区不存在")
		return
	}
	archive := map[string]any{"community_id": id, "community_name": name}

	var ordersTotal, ordersClosed, treatmentsTotal int
	db.QueryRow(`SELECT count(*) FROM work_orders WHERE community_id=$1`, id).Scan(&ordersTotal)
	db.QueryRow(`SELECT count(*) FROM work_orders WHERE community_id=$1 AND status='closed'`, id).Scan(&ordersClosed)
	db.QueryRow(`SELECT count(*) FROM treatments t JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=$1`, id).Scan(&treatmentsTotal)
	archive["orders_total"] = ordersTotal
	archive["orders_closed"] = ordersClosed
	archive["treatments_total"] = treatmentsTotal

	// 药剂消耗
	type chemUsed struct {
		Name string  `json:"name"`
		Used float64 `json:"used"`
	}
	chems := []chemUsed{}
	crows, err := db.Query(`SELECT t.chemical_name, sum(t.chemical_used) FROM treatments t
		JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=$1 GROUP BY t.chemical_name ORDER BY 2 DESC`, id)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var cu chemUsed
			crows.Scan(&cu.Name, &cu.Used)
			chems = append(chems, cu)
		}
	}
	archive["chemical_usage"] = chems

	// 宠物投诉统计与告知调整记录
	var total, pending, revisit, compensation int
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1`, id).Scan(&total)
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND status='pending'`, id).Scan(&pending)
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND need_revisit`, id).Scan(&revisit)
	db.QueryRow(`SELECT count(*) FROM pet_complaints WHERE community_id=$1 AND compensation`, id).Scan(&compensation)
	archive["pet_complaints"] = map[string]int{
		"total": total, "pending": pending, "need_revisit": revisit, "compensation": compensation,
	}
	type adj struct {
		ComplaintNo string     `json:"complaint_no"`
		Adjustment  string     `json:"notify_adjustment"`
		HandledAt   *time.Time `json:"handled_at"`
	}
	adjustments := []adj{}
	arows, err := db.Query(`SELECT complaint_no, notify_adjustment, handled_at FROM pet_complaints
		WHERE community_id=$1 AND notify_adjustment != '' ORDER BY handled_at DESC`, id)
	if err == nil {
		defer arows.Close()
		for arows.Next() {
			var a adj
			arows.Scan(&a.ComplaintNo, &a.Adjustment, &a.HandledAt)
			adjustments = append(adjustments, a)
		}
	}
	archive["notify_adjustments"] = adjustments
	archive["pet_tracking"] = petTrackingForCommunity(id)

	// 最近居民告知
	type note struct {
		Title     string    `json:"title"`
		Content   string    `json:"content"`
		CreatedAt time.Time `json:"created_at"`
	}
	notes := []note{}
	nrows, err := db.Query(`SELECT title, content, created_at FROM notifications WHERE community_id=$1 ORDER BY id DESC LIMIT 5`, id)
	if err == nil {
		defer nrows.Close()
		for nrows.Next() {
			var n note
			nrows.Scan(&n.Title, &n.Content, &n.CreatedAt)
			notes = append(notes, n)
		}
	}
	archive["recent_notifications"] = notes

	// 重点风险期应急响应归档（含风险等级、处置结果，供街道考核/后续计划/追溯）
	type emergArc struct {
		EmergNo       string     `json:"emerg_no"`
		Title         string     `json:"title"`
		TriggerType   string     `json:"trigger_type"`
		TriggerLabel  string     `json:"trigger_label"`
		Disease       string     `json:"disease"`
		Status        string     `json:"status"`
		RiskLevel     string     `json:"risk_level"`
		RecheckDays   int        `json:"recheck_interval_days"`
		StartedAt     time.Time  `json:"started_at"`
		ResolvedAt    *time.Time `json:"resolved_at"`
		ResolveNote   string     `json:"resolve_note"`
		CmpDelta      int        `json:"complaint_delta"`
	}
	emList := []emergArc{}
	erows, err := db.Query(`SELECT e.emerg_no, e.title, e.trigger_type, e.disease, e.status, ec.risk_level,
		e.recheck_interval_days, e.started_at, e.resolved_at, COALESCE(e.resolve_note,''),
		(SELECT count(*) FROM reports WHERE community_id=$1) - ec.complaints_at_start
		FROM emergency_responses e JOIN emergency_communities ec ON ec.emergency_id=e.id
		WHERE ec.community_id=$1 ORDER BY e.id DESC LIMIT 20`, id)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var x emergArc
			erows.Scan(&x.EmergNo, &x.Title, &x.TriggerType, &x.Disease, &x.Status, &x.RiskLevel,
				&x.RecheckDays, &x.StartedAt, &x.ResolvedAt, &x.ResolveNote, &x.CmpDelta)
			x.TriggerLabel = labelOf(EmergTriggerLabels, x.TriggerType)
			emList = append(emList, x)
		}
	}
	archive["emergencies"] = emList
	var riskLevel, riskReason string
	db.QueryRow(`SELECT risk_level, risk_reason FROM communities WHERE id=$1`, id).Scan(&riskLevel, &riskReason)
	archive["risk_level"] = riskLevel
	archive["risk_level_label"] = labelOf(CommunityRiskLabels, riskLevel)
	archive["risk_reason"] = riskReason

	jsonOK(c.W, archive)
}
