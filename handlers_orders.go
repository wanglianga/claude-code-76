package main

import (
	"database/sql"
	"fmt"
	"time"
)

func registerOrderRoutes() {
	handle("GET /api/work-orders", hListOrders)
	handle("GET /api/work-orders/{id}", hGetOrder)
	handle("POST /api/work-orders/{id}/assign", hAssignOrder, "street")
	handle("POST /api/work-orders/{id}/start", hStartOrder, "operator")
	handle("POST /api/work-orders/{id}/treatments", hCreateTreatment, "operator")
	handle("POST /api/work-orders/{id}/rechecks", hCreateRecheck, "operator", "grid", "supervisor", "street")
	handle("POST /api/work-orders/{id}/issues", hCreateIssue)
	handle("POST /api/work-orders/{id}/issues/{issueId}/resolve", hResolveIssue, "street", "supervisor")
	handle("POST /api/work-orders/{id}/rectifications", hCreateRectification, "street")
	handle("POST /api/rectifications/{id}/start", hStartRectification, "property")
	handle("POST /api/rectifications/{id}/complete", hCompleteRectification, "property")
	handle("POST /api/rectifications/{id}/verify", hVerifyRectification, "street", "supervisor")
	handle("GET /api/rectifications", hListRectifications)
	handle("POST /api/work-orders/{id}/close", hCloseOrder, "street")
}

// ---------- 工单共享辅助 ----------

func addLog(orderID int64, actor *SessionUser, action, content string) {
	var uid any
	role := "system"
	if actor != nil {
		uid = actor.ID
		role = actor.Role
	}
	db.Exec(`INSERT INTO work_order_logs(work_order_id, actor_id, actor_role, action, content) VALUES($1,$2,$3,$4,$5)`,
		orderID, uid, role, action, content)
}

func ensureParty(orderID int64, role string, userID *int64, note string) {
	db.Exec(`INSERT INTO work_order_parties(work_order_id, party_role, user_id, note) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		orderID, role, userID, note)
}

func addTeamParties(orderID, teamID int64) {
	rows, err := db.Query(`SELECT id FROM users WHERE role='operator' AND team_id=$1`, teamID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ensureParty(orderID, "operator", &id, "消杀队员")
	}
}

func addCommunityPropertyParties(orderID, communityID int64) {
	rows, err := db.Query(`SELECT id FROM users WHERE role='property' AND community_id=$1`, communityID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ensureParty(orderID, "property", &id, "小区物业")
	}
}

// ensureAllParties 异常升级时把居民、物业、消杀队、街道、卫生监督拉到同一工单
func ensureAllParties(orderID int64) {
	var communityID int64
	var reportID, teamID, createdBy sql.NullInt64
	if err := db.QueryRow(`SELECT community_id, report_id, team_id, created_by FROM work_orders WHERE id=$1`, orderID).
		Scan(&communityID, &reportID, &teamID, &createdBy); err != nil {
		return
	}
	if reportID.Valid {
		var rid int64
		if db.QueryRow(`SELECT reporter_id FROM reports WHERE id=$1`, reportID.Int64).Scan(&rid) == nil {
			ensureParty(orderID, "resident", &rid, "上报居民")
		}
	} else {
		// 积水点工单无上报人时，纳入本小区居民代表协同
		rows, err := db.Query(`SELECT id FROM users WHERE role='resident' AND community_id=$1`, communityID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int64
				rows.Scan(&id)
				ensureParty(orderID, "resident", &id, "小区居民代表")
			}
		}
	}
	addCommunityPropertyParties(orderID, communityID)
	if teamID.Valid {
		addTeamParties(orderID, teamID.Int64)
	}
	if createdBy.Valid {
		id := createdBy.Int64
		ensureParty(orderID, "street", &id, "派单街道")
	} else {
		var sid int64
		if db.QueryRow(`SELECT id FROM users WHERE role='street' ORDER BY id LIMIT 1`).Scan(&sid) == nil {
			ensureParty(orderID, "street", &sid, "街道")
		}
	}
	var supID int64
	if db.QueryRow(`SELECT id FROM users WHERE role='supervisor' ORDER BY id LIMIT 1`).Scan(&supID) == nil {
		ensureParty(orderID, "supervisor", &supID, "卫生监督")
	}
}

// createIssue 在工单内登记异常并升级为多方协同
func createIssue(orderID int64, actor *SessionUser, issueType, desc string) int64 {
	var id int64
	var raisedBy any
	if actor != nil {
		raisedBy = actor.ID
	}
	if err := db.QueryRow(`INSERT INTO issues(work_order_id, type, description, raised_by) VALUES($1,$2,$3,$4) RETURNING id`,
		orderID, issueType, desc, raisedBy).Scan(&id); err != nil {
		return 0
	}
	db.Exec(`UPDATE work_orders SET status='escalated', updated_at=now() WHERE id=$1 AND status != 'closed'`, orderID)
	ensureAllParties(orderID)
	addLog(orderID, actor, "异常上报", fmt.Sprintf("【%s】%s（已拉通居民/物业/消杀队/街道/卫生监督协同处理）", labelOf(IssueTypes, issueType), desc))
	return id
}

// recheckIntervalDays 重点风险期缩短复查间隔
func recheckIntervalDays() int {
	if rp := activeRiskPeriod(); rp != nil && rp.RecheckIntervalDays > 0 {
		return rp.RecheckIntervalDays
	}
	return 7
}

// closeConditions 返回工单闭环条件达成情况
func closeConditions(orderID int64) map[string]bool {
	conds := map[string]bool{}
	var n int
	db.QueryRow(`SELECT count(*) FROM treatments WHERE work_order_id=$1`, orderID).Scan(&n)
	conds["treatment_done"] = n > 0
	db.QueryRow(`SELECT count(*) FROM rechecks WHERE work_order_id=$1 AND result='pass'`, orderID).Scan(&n)
	conds["recheck_passed"] = n > 0
	db.QueryRow(`SELECT count(*) FROM issues WHERE work_order_id=$1 AND status='open'`, orderID).Scan(&n)
	conds["no_open_issues"] = n == 0
	db.QueryRow(`SELECT count(*) FROM rectifications WHERE work_order_id=$1 AND status != 'verified'`, orderID).Scan(&n)
	conds["rectifications_verified"] = n == 0
	db.QueryRow(`SELECT count(*) FROM notifications WHERE work_order_id=$1`, orderID).Scan(&n)
	conds["resident_notified"] = n > 0
	return conds
}

func allConditionsMet(conds map[string]bool) bool {
	for _, v := range conds {
		if !v {
			return false
		}
	}
	return true
}

// tryCloseOrder 复查通过等时机自动尝试闭环
func tryCloseOrder(orderID int64, actor *SessionUser) bool {
	conds := closeConditions(orderID)
	if !allConditionsMet(conds) {
		addLog(orderID, actor, "闭环待办", "复查已通过，闭环尚缺："+missingConditions(conds))
		return false
	}
	closeOrder(orderID, actor, "闭环条件全部满足")
	return true
}

func missingConditions(conds map[string]bool) string {
	names := map[string]string{
		"treatment_done": "完成消杀", "recheck_passed": "复查通过",
		"no_open_issues": "异常全部解决", "rectifications_verified": "物业整改核验", "resident_notified": "居民告知",
	}
	s := ""
	for k, v := range conds {
		if !v {
			if s != "" {
				s += "、"
			}
			s += names[k]
		}
	}
	return s
}

func closeOrder(orderID int64, actor *SessionUser, reason string) {
	db.Exec(`UPDATE work_orders SET status='closed', closed_at=now(), updated_at=now() WHERE id=$1`, orderID)
	db.Exec(`UPDATE reports SET status='closed' WHERE id=(SELECT report_id FROM work_orders WHERE id=$1) AND $1 IS NOT NULL`, orderID)
	db.Exec(`UPDATE water_points SET status='cleared' WHERE id=(SELECT water_point_id FROM work_orders WHERE id=$1)`, orderID)
	addLog(orderID, actor, "工单闭环", reason)
	cacheDel("dashboard:closedloop")
}

// ---------- 工单列表 / 详情 ----------

type Order struct {
	ID            int64      `json:"id"`
	OrderNo       string     `json:"order_no"`
	CommunityID   int64      `json:"community_id"`
	CommunityName string     `json:"community_name"`
	WaterPointID  *int64     `json:"water_point_id"`
	ReportID      *int64     `json:"report_id"`
	TeamID        *int64     `json:"team_id"`
	TeamName      *string    `json:"team_name"`
	Priority      float64    `json:"priority"`
	Status        string     `json:"status"`
	StatusLabel   string     `json:"status_label"`
	ScheduledDate *string    `json:"scheduled_date"`
	RecheckDueAt  *time.Time `json:"recheck_due_at"`
	RecheckOverdue bool      `json:"recheck_overdue"`
	SourceDesc    string     `json:"source_desc"`
	CreatedAt     time.Time  `json:"created_at"`
	ClosedAt      *time.Time `json:"closed_at"`
}

const orderSelect = `
	SELECT o.id, o.order_no, o.community_id, cm.name, o.water_point_id, o.report_id, o.team_id, t.name,
	       o.priority, o.status, to_char(o.scheduled_date,'YYYY-MM-DD'), o.recheck_due_at,
	       COALESCE(r.location_desc, w.location_desc, ''), COALESCE(r.type, w.type, ''),
	       o.created_at, o.closed_at, r.report_no
	FROM work_orders o
	JOIN communities cm ON cm.id=o.community_id
	LEFT JOIN teams t ON t.id=o.team_id
	LEFT JOIN reports r ON r.id=o.report_id
	LEFT JOIN water_points w ON w.id=o.water_point_id`

func scanOrder(row interface{ Scan(...any) error }) (*Order, error) {
	var o Order
	var srcLoc, srcType string
	var reportNo *string
	err := row.Scan(&o.ID, &o.OrderNo, &o.CommunityID, &o.CommunityName, &o.WaterPointID, &o.ReportID,
		&o.TeamID, &o.TeamName, &o.Priority, &o.Status, &o.ScheduledDate, &o.RecheckDueAt,
		&srcLoc, &srcType, &o.CreatedAt, &o.ClosedAt, &reportNo)
	if err != nil {
		return nil, err
	}
	o.StatusLabel = labelOf(OrderStatusLabels, o.Status)
	if reportNo != nil {
		o.SourceDesc = *reportNo + " "
	}
	if lbl, ok := ReportTypes[srcType]; ok {
		o.SourceDesc += lbl + " · "
	} else if lbl, ok := WaterPointTypes[srcType]; ok {
		o.SourceDesc += lbl + " · "
	}
	o.SourceDesc += srcLoc
	if o.RecheckDueAt != nil && o.Status == "recheck_pending" && o.RecheckDueAt.Before(time.Now()) {
		o.RecheckOverdue = true
	}
	return &o, nil
}

func hListOrders(c *Ctx) {
	q := orderSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	switch c.User.Role {
	case "operator":
		if c.User.TeamID != nil {
			q += ` AND o.team_id=` + next()
			args = append(args, *c.User.TeamID)
		}
	case "property", "grid":
		if c.User.CommunityID != nil {
			q += ` AND o.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "resident":
		q += ` AND o.report_id IN (SELECT id FROM reports WHERE reporter_id=` + next() + `)`
		args = append(args, c.User.ID)
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND o.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND o.community_id=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY o.status='closed', o.priority DESC, o.id DESC LIMIT 300`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *o)
	}
	jsonOK(c.W, list)
}

func canViewOrder(u *SessionUser, o *Order) bool {
	switch u.Role {
	case "street", "supervisor":
		return true
	case "operator":
		return u.TeamID != nil && o.TeamID != nil && *u.TeamID == *o.TeamID
	case "property", "grid":
		return u.CommunityID != nil && *u.CommunityID == o.CommunityID
	case "resident":
		if o.ReportID == nil {
			return false
		}
		var rid int64
		if db.QueryRow(`SELECT reporter_id FROM reports WHERE id=$1`, *o.ReportID).Scan(&rid) == nil {
			return rid == u.ID
		}
	}
	return false
}

func hGetOrder(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	row := db.QueryRow(orderSelect+` WHERE o.id=$1`, id)
	o, err := scanOrder(row)
	if err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if !canViewOrder(c.User, o) {
		jsonErr(c.W, 403, "无权查看该工单")
		return
	}
	detail := map[string]any{"order": o, "close_conditions": closeConditions(id)}

	if o.ReportID != nil {
		if rep, err := scanReport(db.QueryRow(reportSelect+` WHERE r.id=$1`, *o.ReportID)); err == nil {
			detail["report"] = rep
		}
	}
	if o.WaterPointID != nil {
		if wp, err := scanWaterPoint(db.QueryRow(waterPointSelect+` WHERE w.id=$1`, *o.WaterPointID)); err == nil {
			detail["water_point"] = wp
		}
	}

	// 参与方
	type Party struct {
		Role      string    `json:"role"`
		RoleLabel string    `json:"role_label"`
		UserID    *int64    `json:"user_id"`
		UserName  *string   `json:"user_name"`
		Note      string    `json:"note"`
		JoinedAt  time.Time `json:"joined_at"`
	}
	parties := []Party{}
	prows, err := db.Query(`SELECT p.party_role, p.user_id, u.name, p.note, p.joined_at
		FROM work_order_parties p LEFT JOIN users u ON u.id=p.user_id WHERE p.work_order_id=$1 ORDER BY p.id`, id)
	if err == nil {
		defer prows.Close()
		for prows.Next() {
			var p Party
			prows.Scan(&p.Role, &p.UserID, &p.UserName, &p.Note, &p.JoinedAt)
			p.RoleLabel = labelOf(RoleLabels, p.Role)
			parties = append(parties, p)
		}
	}
	detail["parties"] = parties

	// 消杀记录
	type Treatment struct {
		ID                int64     `json:"id"`
		OperatorName      string    `json:"operator_name"`
		ChemicalName      string    `json:"chemical_name"`
		Concentration     string    `json:"concentration"`
		SprayArea         string    `json:"spray_area"`
		WarningSign       bool      `json:"warning_sign"`
		ResidentNotified  bool      `json:"resident_notified"`
		PetAvoided        bool      `json:"pet_avoided"`
		NewWaterPoint     bool      `json:"new_water_point_found"`
		NewWaterPointDesc string    `json:"new_water_point_desc"`
		ChemicalUsed      float64   `json:"chemical_used"`
		TreatedAt         time.Time `json:"treated_at"`
	}
	treatments := []Treatment{}
	trows, err := db.Query(`SELECT t.id, u.name, t.chemical_name, t.concentration, t.spray_area, t.warning_sign,
		t.resident_notified, t.pet_avoided, t.new_water_point_found, t.new_water_point_desc, t.chemical_used, t.treated_at
		FROM treatments t JOIN users u ON u.id=t.operator_id WHERE t.work_order_id=$1 ORDER BY t.id`, id)
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var t Treatment
			trows.Scan(&t.ID, &t.OperatorName, &t.ChemicalName, &t.Concentration, &t.SprayArea, &t.WarningSign,
				&t.ResidentNotified, &t.PetAvoided, &t.NewWaterPoint, &t.NewWaterPointDesc, &t.ChemicalUsed, &t.TreatedAt)
			treatments = append(treatments, t)
		}
	}
	detail["treatments"] = treatments

	// 复查记录
	type Recheck struct {
		ID            int64     `json:"id"`
		InspectorName string    `json:"inspector_name"`
		Result        string    `json:"result"`
		ResultLabel   string    `json:"result_label"`
		LarvaeFound   bool      `json:"larvae_found"`
		Notes         string    `json:"notes"`
		CreatedAt     time.Time `json:"created_at"`
	}
	rechecks := []Recheck{}
	rrows, err := db.Query(`SELECT r.id, u.name, r.result, r.larvae_found, r.notes, r.created_at
		FROM rechecks r JOIN users u ON u.id=r.inspector_id WHERE r.work_order_id=$1 ORDER BY r.id`, id)
	if err == nil {
		defer rrows.Close()
		for rrows.Next() {
			var r Recheck
			rrows.Scan(&r.ID, &r.InspectorName, &r.Result, &r.LarvaeFound, &r.Notes, &r.CreatedAt)
			if r.Result == "pass" {
				r.ResultLabel = "通过"
			} else {
				r.ResultLabel = "发现幼虫"
			}
			rechecks = append(rechecks, r)
		}
	}
	detail["rechecks"] = rechecks

	// 异常
	type Issue struct {
		ID          int64      `json:"id"`
		Type        string     `json:"type"`
		TypeLabel   string     `json:"type_label"`
		Description string     `json:"description"`
		Status      string     `json:"status"`
		StatusLabel string     `json:"status_label"`
		RaisedBy    *string    `json:"raised_by"`
		Resolution  string     `json:"resolution"`
		CreatedAt   time.Time  `json:"created_at"`
		ResolvedAt  *time.Time `json:"resolved_at"`
	}
	issues := []Issue{}
	irows, err := db.Query(`SELECT i.id, i.type, i.description, i.status, u.name, i.resolution, i.created_at, i.resolved_at
		FROM issues i LEFT JOIN users u ON u.id=i.raised_by WHERE i.work_order_id=$1 ORDER BY i.id`, id)
	if err == nil {
		defer irows.Close()
		for irows.Next() {
			var x Issue
			irows.Scan(&x.ID, &x.Type, &x.Description, &x.Status, &x.RaisedBy, &x.Resolution, &x.CreatedAt, &x.ResolvedAt)
			x.TypeLabel = labelOf(IssueTypes, x.Type)
			x.StatusLabel = labelOf(IssueStatusLabels, x.Status)
			issues = append(issues, x)
		}
	}
	detail["issues"] = issues

	// 物业整改
	type Rect struct {
		ID             int64      `json:"id"`
		PropertyUserID int64      `json:"property_user_id"`
		PropertyName   string     `json:"property_name"`
		Description    string     `json:"description"`
		Deadline       *string    `json:"deadline"`
		Status         string     `json:"status"`
		StatusLabel    string     `json:"status_label"`
		CompletedAt    *time.Time `json:"completed_at"`
		VerifyResult   string     `json:"verify_result"`
		CreatedAt      time.Time  `json:"created_at"`
	}
	rects := []Rect{}
	rcrows, err := db.Query(`SELECT r.id, r.property_user_id, u.name, r.description, to_char(r.deadline,'YYYY-MM-DD'),
		r.status, r.completed_at, r.verify_result, r.created_at
		FROM rectifications r JOIN users u ON u.id=r.property_user_id WHERE r.work_order_id=$1 ORDER BY r.id`, id)
	if err == nil {
		defer rcrows.Close()
		for rcrows.Next() {
			var x Rect
			rcrows.Scan(&x.ID, &x.PropertyUserID, &x.PropertyName, &x.Description, &x.Deadline, &x.Status, &x.CompletedAt, &x.VerifyResult, &x.CreatedAt)
			x.StatusLabel = labelOf(RectificationStatusLabels, x.Status)
			rects = append(rects, x)
		}
	}
	detail["rectifications"] = rects

	// 物业积水整改（地下室排水沟长期积水等设施性积水）
	type PPropRect struct {
		ID                int64   `json:"id"`
		RectNo            string  `json:"rect_no"`
		WaterLocation     string  `json:"water_location"`
		WaterTypeLabel    string  `json:"water_type_label"`
		FacilityName      string  `json:"facility_name"`
		RecheckDateStr    string  `json:"recheck_date_str"`
		Status            string  `json:"status"`
		StatusLabel       string  `json:"status_label"`
		RectOverdue       bool    `json:"rect_overdue"`
		RecheckOverdue    bool    `json:"recheck_overdue"`
		RepairMethodLabel string  `json:"repair_method_label"`
		RectifyPhotos     StringList `json:"rectify_photos"`
		RecheckPhotos     StringList `json:"recheck_photos"`
		ComplaintsBefore  int     `json:"complaints_before"`
		ComplaintsAfter   int     `json:"complaints_after"`
	}
	ppRects := []PPropRect{}
	if pr, err := db.Query(`SELECT id, rect_no, water_location, water_type, facility_user_id, recheck_date, status,
		COALESCE(repair_method,''), COALESCE(rectify_photos,'[]'::jsonb), COALESCE(recheck_photos,'[]'::jsonb),
		complaints_before, complaints_after
		FROM property_rectifications WHERE work_order_id=$1 ORDER BY id`, id); err == nil {
		defer pr.Close()
		for pr.Next() {
			var x PPropRect
			var facilityID int64
			var wt string
			var rd time.Time
			var status string
			pr.Scan(&x.ID, &x.RectNo, &x.WaterLocation, &wt, &facilityID, &rd, &status,
				&x.RepairMethodLabel, &x.RectifyPhotos, &x.RecheckPhotos, &x.ComplaintsBefore, &x.ComplaintsAfter)
			x.WaterTypeLabel = labelOf(WaterPointTypes, wt)
			x.RepairMethodLabel = labelOf(PropRectMethodLabels, x.RepairMethodLabel)
			db.QueryRow(`SELECT name FROM users WHERE id=$1`, facilityID).Scan(&x.FacilityName)
			x.RecheckDateStr = rd.Format("2006-01-02")
			x.Status = status
			x.StatusLabel = labelOf(PropRectStatusLabels, status)
			if rd.Before(time.Now().Truncate(24*time.Hour)) {
				if status == "pending" || status == "rectifying" || status == "rejected" {
					x.RectOverdue = true
				} else if status == "recheck_pending" {
					x.RecheckOverdue = true
				}
			}
			ppRects = append(ppRects, x)
		}
	}
	detail["property_rectifications"] = ppRects

	// 时间线
	type Log struct {
		ActorName  *string   `json:"actor_name"`
		ActorRole  string    `json:"actor_role"`
		RoleLabel  string    `json:"role_label"`
		Action     string    `json:"action"`
		Content    string    `json:"content"`
		CreatedAt  time.Time `json:"created_at"`
	}
	logs := []Log{}
	lrows, err := db.Query(`SELECT u.name, l.actor_role, l.action, l.content, l.created_at
		FROM work_order_logs l LEFT JOIN users u ON u.id=l.actor_id WHERE l.work_order_id=$1 ORDER BY l.id`, id)
	if err == nil {
		defer lrows.Close()
		for lrows.Next() {
			var l Log
			lrows.Scan(&l.ActorName, &l.ActorRole, &l.Action, &l.Content, &l.CreatedAt)
			l.RoleLabel = labelOf(RoleLabels, l.ActorRole)
			logs = append(logs, l)
		}
	}
	detail["logs"] = logs

	jsonOK(c.W, detail)
}

// ---------- 工单操作 ----------

func hAssignOrder(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		TeamID        int64  `json:"team_id"`
		ScheduledDate string `json:"scheduled_date"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.TeamID == 0 || req.ScheduledDate == "" {
		jsonErr(c.W, 400, "请选择消杀队与计划日期")
		return
	}
	res, err := db.Exec(`UPDATE work_orders SET team_id=$1, scheduled_date=$2, status='assigned', updated_at=now()
		WHERE id=$3 AND status IN ('pending','assigned')`, req.TeamID, req.ScheduledDate, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "工单状态不允许派单（仅待派单/已派单可调整）")
		return
	}
	addTeamParties(id, req.TeamID)
	var tname string
	db.QueryRow(`SELECT name FROM teams WHERE id=$1`, req.TeamID).Scan(&tname)
	addLog(id, c.User, "指派消杀队", fmt.Sprintf("指派「%s」，计划 %s 到场", tname, req.ScheduledDate))
	jsonOK(c.W, map[string]string{"message": "已指派 " + tname})
}

func hStartOrder(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	res, err := db.Exec(`UPDATE work_orders SET status='in_progress', updated_at=now()
		WHERE id=$1 AND status IN ('assigned','pending','escalated') AND (team_id=$2 OR team_id IS NULL)`, id, c.User.TeamID)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "工单不在可开始状态，或不属于您所在消杀队")
		return
	}
	db.Exec(`UPDATE reports SET status='processing' WHERE id=(SELECT report_id FROM work_orders WHERE id=$1)`, id)
	addLog(id, c.User, "到场开工", "消杀人员已到场开始作业")
	jsonOK(c.W, map[string]string{"message": "已开工"})
}

func hCreateTreatment(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		ChemicalID        int64   `json:"chemical_id"`
		Concentration     string  `json:"concentration"`
		SprayArea         string  `json:"spray_area"`
		WarningSign       bool    `json:"warning_sign"`
		ResidentNotified  bool    `json:"resident_notified"`
		PetAvoided        bool    `json:"pet_avoided"`
		NewWaterPoint     bool    `json:"new_water_point_found"`
		NewWaterPointDesc string  `json:"new_water_point_desc"`
		ChemicalUsed      float64 `json:"chemical_used"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.ChemicalID == 0 || req.SprayArea == "" || req.ChemicalUsed <= 0 {
		jsonErr(c.W, 400, "请填写药剂、喷洒区域与用量")
		return
	}
	var status string
	var wpID sql.NullInt64
	if err := db.QueryRow(`SELECT status, water_point_id FROM work_orders WHERE id=$1`, id).Scan(&status, &wpID); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	allowed := map[string]bool{"assigned": true, "in_progress": true, "escalated": true, "rectifying": true, "recheck_pending": true}
	if !allowed[status] {
		jsonErr(c.W, 409, "当前状态不允许提交消杀记录")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer tx.Rollback()
	var stock float64
	var chemName string
	if err := tx.QueryRow(`SELECT stock, name FROM chemicals WHERE id=$1 FOR UPDATE`, req.ChemicalID).Scan(&stock, &chemName); err != nil {
		jsonErr(c.W, 400, "药剂不存在")
		return
	}
	if stock < req.ChemicalUsed {
		tx.Rollback()
		createIssue(id, c.User, "chemical_shortage",
			fmt.Sprintf("消杀需用 %s %.1f，库存仅 %.1f，药剂不足", chemName, req.ChemicalUsed, stock))
		jsonErr(c.W, 409, fmt.Sprintf("药剂库存不足（%s 剩余 %.1f），已自动生成「药剂不足」异常工单事项", chemName, stock))
		return
	}
	if _, err := tx.Exec(`UPDATE chemicals SET stock=stock-$1, updated_at=now() WHERE id=$2`, req.ChemicalUsed, req.ChemicalID); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`INSERT INTO treatments(work_order_id, operator_id, chemical_id, chemical_name, concentration, spray_area,
		warning_sign, resident_notified, pet_avoided, new_water_point_found, new_water_point_desc, chemical_used)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		id, c.User.ID, req.ChemicalID, chemName, req.Concentration, req.SprayArea,
		req.WarningSign, req.ResidentNotified, req.PetAvoided, req.NewWaterPoint, req.NewWaterPointDesc, req.ChemicalUsed); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if req.NewWaterPoint && req.NewWaterPointDesc != "" {
		var commID int64
		tx.QueryRow(`SELECT community_id FROM work_orders WHERE id=$1`, id).Scan(&commID)
		if _, err := tx.Exec(`INSERT INTO water_points(community_id, type, location_desc, source, discovered_by)
			VALUES($1,'other',$2,'treatment',$3)`, commID, req.NewWaterPointDesc, c.User.ID); err != nil {
			jsonErr(c.W, 500, "新积水点登记失败: "+err.Error())
			return
		}
	}
	due := time.Now().AddDate(0, 0, recheckIntervalDays())
	if _, err := tx.Exec(`UPDATE work_orders SET status='recheck_pending', recheck_due_at=$1, updated_at=now() WHERE id=$2`, due, id); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if wpID.Valid {
		tx.Exec(`UPDATE water_points SET status='recheck_pending', last_treated_at=now() WHERE id=$1`, wpID.Int64)
	}
	if err := tx.Commit(); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	// 关联的儿童活动区错峰计划进入「已作业」，待警示撤除与园方确认
	if res, err := db.Exec(`UPDATE child_zone_plans SET status='treated' WHERE work_order_id=$1 AND status IN ('planned','notified')`, id); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			addLog(id, nil, "错峰消杀作业完成", "儿童活动区作业已完成，待警示撤除与园方确认")
		}
	}
	addLog(id, c.User, "消杀完成", fmt.Sprintf("药剂 %s（浓度 %s）用量 %.1f，喷洒区域：%s；警示牌:%v 居民告知:%v 宠物避让:%v；%d 天后复查",
		chemName, req.Concentration, req.ChemicalUsed, req.SprayArea, req.WarningSign, req.ResidentNotified, req.PetAvoided, recheckIntervalDays()))
	// 地下室排水沟长期积水：消杀队只能临时处理，同一积水点反复处理时系统自动生成物业整改任务
	maybeCreatePropertyRect(id, c.User)
	if req.NewWaterPoint {
		addLog(id, c.User, "发现新积水点", req.NewWaterPointDesc)
	}
	// 库存低于安全线 → 自动登记「药剂不足」异常提醒街道
	var newStock, safeStock float64
	db.QueryRow(`SELECT stock, safe_stock FROM chemicals WHERE id=$1`, req.ChemicalID).Scan(&newStock, &safeStock)
	if newStock < safeStock {
		createIssue(id, c.User, "chemical_shortage",
			fmt.Sprintf("%s 库存 %.1f 已低于安全库存 %.1f，请街道补货", chemName, newStock, safeStock))
	}
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "消杀记录已提交，工单转入待复查"})
}

func hCreateRecheck(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Result      string `json:"result"` // pass | larvae_found
		LarvaeFound bool   `json:"larvae_found"`
		Notes       string `json:"notes"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.Result != "pass" && req.Result != "larvae_found" {
		jsonErr(c.W, 400, "复查结果须为 pass 或 larvae_found")
		return
	}
	if req.Result == "larvae_found" {
		req.LarvaeFound = true
	}
	var status string
	var wpID sql.NullInt64
	if err := db.QueryRow(`SELECT status, water_point_id FROM work_orders WHERE id=$1`, id).Scan(&status, &wpID); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if status == "closed" {
		jsonErr(c.W, 409, "工单已闭环")
		return
	}
	if _, err := db.Exec(`INSERT INTO rechecks(work_order_id, water_point_id, inspector_id, result, larvae_found, notes)
		VALUES($1,$2,$3,$4,$5,$6)`, id, wpID, c.User.ID, req.Result, req.LarvaeFound, req.Notes); err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if wpID.Valid {
		db.Exec(`UPDATE water_points SET last_recheck_at=now() WHERE id=$1`, wpID.Int64)
	}
	if req.Result == "pass" {
		if wpID.Valid {
			db.Exec(`UPDATE water_points SET status='cleared', larvae_found=false WHERE id=$1`, wpID.Int64)
		}
		addLog(id, c.User, "复查通过", req.Notes)
		if tryCloseOrder(id, c.User) {
			jsonOK(c.W, map[string]string{"message": "复查通过，工单已自动闭环"})
			return
		}
		jsonOK(c.W, map[string]string{"message": "复查通过，待闭环条件全部满足后街道可闭环"})
		return
	}
	// 复查仍有幼虫 → 升级协同
	if wpID.Valid {
		db.Exec(`UPDATE water_points SET larvae_found=true, status='treating' WHERE id=$1`, wpID.Int64)
	}
	createIssue(id, c.User, "larvae_remaining", "复查发现仍有幼虫："+req.Notes)
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "复查发现幼虫，工单已升级为异常协同处理"})
}

func hCreateIssue(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if _, ok := IssueTypes[req.Type]; !ok {
		jsonErr(c.W, 400, "异常类型非法")
		return
	}
	if req.Description == "" {
		jsonErr(c.W, 400, "请填写异常描述")
		return
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=$1`, id).Scan(&status); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if status == "closed" {
		jsonErr(c.W, 409, "工单已闭环，无法上报异常")
		return
	}
	issueID := createIssue(id, c.User, req.Type, req.Description)
	jsonOK(c.W, map[string]any{"id": issueID, "message": "异常已登记，工单进入多方协同处理"})
}

func hResolveIssue(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	issueID, ok := pathID(c, "issueId")
	if !ok {
		return
	}
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Resolution == "" {
		jsonErr(c.W, 400, "请填写处理结果")
		return
	}
	res, err := db.Exec(`UPDATE issues SET status='resolved', handled_by=$1, resolution=$2, resolved_at=now()
		WHERE id=$3 AND work_order_id=$4 AND status='open'`, c.User.ID, req.Resolution, issueID, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 404, "异常不存在或已解决")
		return
	}
	addLog(id, c.User, "异常解决", req.Resolution)
	var open int
	db.QueryRow(`SELECT count(*) FROM issues WHERE work_order_id=$1 AND status='open'`, id).Scan(&open)
	if open == 0 {
		db.Exec(`UPDATE work_orders SET status='in_progress', updated_at=now() WHERE id=$1 AND status='escalated'`, id)
		addLog(id, nil, "协同完成", "全部异常已解决，工单恢复处理")
	}
	jsonOK(c.W, map[string]string{"message": "异常已标记解决"})
}

// ---------- 物业整改 ----------

func hCreateRectification(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		PropertyUserID int64  `json:"property_user_id"`
		Description    string `json:"description"`
		Deadline       string `json:"deadline"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Description == "" {
		jsonErr(c.W, 400, "请填写整改要求")
		return
	}
	var commID int64
	var wpID sql.NullInt64
	if err := db.QueryRow(`SELECT community_id, water_point_id FROM work_orders WHERE id=$1`, id).Scan(&commID, &wpID); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if req.PropertyUserID == 0 {
		db.QueryRow(`SELECT id FROM users WHERE role='property' AND community_id=$1 ORDER BY id LIMIT 1`, commID).Scan(&req.PropertyUserID)
	}
	if req.PropertyUserID == 0 {
		jsonErr(c.W, 400, "该小区暂无物业账号，请指定整改负责人")
		return
	}
	var deadline any
	if req.Deadline != "" {
		deadline = req.Deadline
	}
	var rectID int64
	err := db.QueryRow(`INSERT INTO rectifications(work_order_id, water_point_id, property_user_id, description, deadline)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, id, wpID, req.PropertyUserID, req.Description, deadline).Scan(&rectID)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE work_orders SET status='rectifying', updated_at=now() WHERE id=$1 AND status != 'closed'`, id)
	if wpID.Valid {
		db.Exec(`UPDATE water_points SET status='rectifying' WHERE id=$1`, wpID.Int64)
	}
	ensureParty(id, "property", &req.PropertyUserID, "整改责任物业")
	addLog(id, c.User, "发起物业整改", req.Description)
	jsonOK(c.W, map[string]any{"id": rectID, "message": "整改任务已下发物业"})
}

func hStartRectification(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	res, err := db.Exec(`UPDATE rectifications SET status='in_progress' WHERE id=$1 AND property_user_id=$2 AND status='pending'`, id, c.User.ID)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "整改任务不存在或不属于您")
		return
	}
	var orderID int64
	db.QueryRow(`SELECT work_order_id FROM rectifications WHERE id=$1`, id).Scan(&orderID)
	addLog(orderID, c.User, "物业受理整改", "物业已进场整改")
	jsonOK(c.W, map[string]string{"message": "已受理"})
}

func hCompleteRectification(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	res, err := db.Exec(`UPDATE rectifications SET status='done', completed_at=now() WHERE id=$1 AND property_user_id=$2 AND status IN ('pending','in_progress')`, id, c.User.ID)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "整改任务不存在、不属于您或已核验")
		return
	}
	var orderID int64
	db.QueryRow(`SELECT work_order_id FROM rectifications WHERE id=$1`, id).Scan(&orderID)
	addLog(orderID, c.User, "物业完成整改", "整改完成，待街道/卫生监督核验")
	jsonOK(c.W, map[string]string{"message": "已提交完成，等待核验"})
}

func hVerifyRectification(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Pass  bool   `json:"pass"`
		Notes string `json:"notes"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	var orderID int64
	var wpID sql.NullInt64
	var status string
	if err := db.QueryRow(`SELECT work_order_id, water_point_id, status FROM rectifications WHERE id=$1`, id).Scan(&orderID, &wpID, &status); err != nil {
		jsonErr(c.W, 404, "整改任务不存在")
		return
	}
	if status != "done" {
		jsonErr(c.W, 409, "仅「待核验」状态的整改可核验")
		return
	}
	if req.Pass {
		db.Exec(`UPDATE rectifications SET status='verified', verify_result=$1, verified_by=$2, verified_at=now() WHERE id=$3`, req.Notes, c.User.ID, id)
		addLog(orderID, c.User, "整改核验通过", req.Notes)
		// 整改核验通过 → 积水点回到待复查，复查间隔按风险期
		due := time.Now().AddDate(0, 0, recheckIntervalDays())
		if wpID.Valid {
			db.Exec(`UPDATE water_points SET status='recheck_pending' WHERE id=$1`, wpID.Int64)
		}
		db.Exec(`UPDATE work_orders SET status='recheck_pending', recheck_due_at=$1, updated_at=now() WHERE id=$2`, due, orderID)
		addLog(orderID, nil, "待复查", "整改核验通过，进入复查流程")
	} else {
		db.Exec(`UPDATE rectifications SET status='in_progress', verify_result=$1, verified_by=$2 WHERE id=$3`, "核验不通过："+req.Notes, c.User.ID, id)
		addLog(orderID, c.User, "整改核验不通过", req.Notes)
	}
	jsonOK(c.W, map[string]string{"message": "核验结果已记录"})
}

func hListRectifications(c *Ctx) {
	q := `SELECT r.id, r.work_order_id, o.order_no, r.property_user_id, u.name, cm.name, r.description,
		to_char(r.deadline,'YYYY-MM-DD'), r.status, r.completed_at, r.verify_result, r.created_at
		FROM rectifications r
		JOIN work_orders o ON o.id=r.work_order_id
		JOIN users u ON u.id=r.property_user_id
		JOIN communities cm ON cm.id=o.community_id WHERE 1=1`
	args := []any{}
	if c.User.Role == "property" {
		q += ` AND r.property_user_id=$1`
		args = append(args, c.User.ID)
	} else if c.User.Role == "grid" && c.User.CommunityID != nil {
		q += ` AND o.community_id=$1`
		args = append(args, *c.User.CommunityID)
	} else if c.User.Role == "resident" || c.User.Role == "operator" {
		jsonOK(c.W, []any{})
		return
	}
	q += ` ORDER BY r.status='verified', r.id DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type R struct {
		ID             int64      `json:"id"`
		WorkOrderID    int64      `json:"work_order_id"`
		OrderNo        string     `json:"order_no"`
		PropertyUserID int64      `json:"property_user_id"`
		PropertyName   string     `json:"property_name"`
		CommunityName  string     `json:"community_name"`
		Description    string     `json:"description"`
		Deadline       *string    `json:"deadline"`
		Status         string     `json:"status"`
		StatusLabel    string     `json:"status_label"`
		CompletedAt    *time.Time `json:"completed_at"`
		VerifyResult   string     `json:"verify_result"`
		CreatedAt      time.Time  `json:"created_at"`
	}
	list := []R{}
	for rows.Next() {
		var x R
		rows.Scan(&x.ID, &x.WorkOrderID, &x.OrderNo, &x.PropertyUserID, &x.PropertyName, &x.CommunityName,
			&x.Description, &x.Deadline, &x.Status, &x.CompletedAt, &x.VerifyResult, &x.CreatedAt)
		x.StatusLabel = labelOf(RectificationStatusLabels, x.Status)
		list = append(list, x)
	}
	jsonOK(c.W, list)
}

// ---------- 街道闭环 ----------

func hCloseOrder(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=$1`, id).Scan(&status); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if status == "closed" {
		jsonErr(c.W, 409, "工单已闭环")
		return
	}
	conds := closeConditions(id)
	if !allConditionsMet(conds) {
		jsonErr(c.W, 409, "闭环条件未满足，尚缺："+missingConditions(conds))
		return
	}
	closeOrder(id, c.User, "街道确认闭环")
	jsonOK(c.W, map[string]string{"message": "工单已闭环"})
}
