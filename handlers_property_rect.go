package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func registerPropertyRectRoutes() {
	handle("GET /api/property-rectifications", hListPropertyRects)
	handle("GET /api/property-rectifications/{id}", hGetPropertyRect)
	// 消杀队/网格员对地下室排水沟长期积水临时处理后，转物业整改；街道也可直接下发
	handle("POST /api/work-orders/{id}/property-rectifications", hCreatePropRectFromOrder, "operator", "grid", "street")
	// 街道/网格员可不经过工单，直接针对地下室积水点建档下发整改
	handle("POST /api/property-rectifications", hCreatePropRect, "street", "grid")
	handle("POST /api/property-rectifications/{id}/start", hStartPropRect, "property")
	handle("POST /api/property-rectifications/{id}/complete", hCompletePropRect, "property")
	handle("POST /api/property-rectifications/{id}/recheck", hRecheckPropRect, "operator", "grid", "street", "supervisor")
	handle("POST /api/property-rectifications/{id}/supervise", hSupervisePropRect, "street")
}

// basementWaterTypes 认定为“地下室/车库排水设施性积水”的积水点类型
var basementWaterTypes = map[string]bool{
	"basement_damp":      true,
	"underground_garage": true,
}

const defaultRectDays = 7 // 物业排水整改默认期限（天），即默认复查日期

// PropRectReminder 督办/超期提醒
type PropRectReminder struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	KindLabel  string    `json:"kind_label"`
	Content    string    `json:"content"`
	RaisedByName string  `json:"raised_by_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// PropRect 物业积水整改任务
type PropRect struct {
	ID                 int64      `json:"id"`
	RectNo             string     `json:"rect_no"`
	WorkOrderID        *int64     `json:"work_order_id"`
	OrderNo            *string    `json:"order_no"`
	WaterPointID       *int64     `json:"water_point_id"`
	CommunityID        int64      `json:"community_id"`
	CommunityName      string     `json:"community_name"`
	WaterLocation      string     `json:"water_location"`
	WaterType          string     `json:"water_type"`
	WaterTypeLabel     string     `json:"water_type_label"`
	TempTreatment      string     `json:"temp_treatment"`
	TempTreatedByName  *string    `json:"temp_treated_by_name"`
	TempTreatedAt      *time.Time `json:"temp_treated_at"`
	TempTreatmentTimes int        `json:"temp_treatment_times"`
	FacilityUserID     int64      `json:"facility_user_id"`
	FacilityName       string     `json:"facility_name"`
	FacilityPhone      string     `json:"facility_phone"`
	RecheckDate        time.Time  `json:"recheck_date"`
	RepairDesc         string     `json:"repair_desc"`
	RepairMethod       string     `json:"repair_method"`
	RepairMethodLabel  string     `json:"repair_method_label"`
	RectifyPhotos      StringList `json:"rectify_photos"`
	RectifyPhotoRemark string     `json:"rectify_photo_remark"`
	RectifiedByName    *string    `json:"rectified_by_name"`
	RectifiedAt        *time.Time `json:"rectified_at"`
	RecheckPhotos      StringList `json:"recheck_photos"`
	RecheckRemark      string     `json:"recheck_remark"`
	RecheckResult      string     `json:"recheck_result"`
	RecheckedByName    *string    `json:"rechecked_by_name"`
	RecheckedAt        *time.Time `json:"rechecked_at"`
	ComplaintsBefore   int        `json:"complaints_before"`
	ComplaintsAfter    int        `json:"complaints_after"`
	Status             string     `json:"status"`
	StatusLabel        string     `json:"status_label"`
	CreatedByName      *string    `json:"created_by_name"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// 派生字段
	RecheckDateStr  string              `json:"recheck_date_str"`
	RectOverdue     bool                `json:"rect_overdue"`
	RecheckOverdue  bool                `json:"recheck_overdue"`
	ComplaintDelta  int                 `json:"complaint_delta"`
	ComplaintDown   *bool               `json:"complaint_decreased"`
	Reminders       []PropRectReminder  `json:"reminders,omitempty"`
	SuperviseCount  int                 `json:"supervise_count"`
}

const propRectSelect = `
	SELECT r.id, r.rect_no, r.work_order_id, o.order_no, r.water_point_id, r.community_id, cm.name,
	       r.water_location, r.water_type, r.temp_treatment, ut.name, r.temp_treated_at, r.temp_treatment_times,
	       r.facility_user_id, uf.name, uf.phone, r.recheck_date,
	       r.repair_desc, r.repair_method, r.rectify_photos, r.rectify_photo_remark, ur.name, r.rectified_at,
	       r.recheck_photos, r.recheck_remark, r.recheck_result, uk.name, r.rechecked_at,
	       r.complaints_before, r.complaints_after, r.status, uc.name, r.created_at, r.updated_at
	FROM property_rectifications r
	JOIN communities cm ON cm.id=r.community_id
	JOIN users uf ON uf.id=r.facility_user_id
	LEFT JOIN work_orders o ON o.id=r.work_order_id
	LEFT JOIN users ut ON ut.id=r.temp_treated_by
	LEFT JOIN users ur ON ur.id=r.rectified_by
	LEFT JOIN users uk ON uk.id=r.rechecked_by
	LEFT JOIN users uc ON uc.id=r.created_by`

func scanPropRect(row interface{ Scan(...any) error }) (*PropRect, error) {
	var p PropRect
	err := row.Scan(&p.ID, &p.RectNo, &p.WorkOrderID, &p.OrderNo, &p.WaterPointID, &p.CommunityID, &p.CommunityName,
		&p.WaterLocation, &p.WaterType, &p.TempTreatment, &p.TempTreatedByName, &p.TempTreatedAt, &p.TempTreatmentTimes,
		&p.FacilityUserID, &p.FacilityName, &p.FacilityPhone, &p.RecheckDate,
		&p.RepairDesc, &p.RepairMethod, &p.RectifyPhotos, &p.RectifyPhotoRemark, &p.RectifiedByName, &p.RectifiedAt,
		&p.RecheckPhotos, &p.RecheckRemark, &p.RecheckResult, &p.RecheckedByName, &p.RecheckedAt,
		&p.ComplaintsBefore, &p.ComplaintsAfter, &p.Status, &p.CreatedByName, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.enrich()
	return &p, nil
}

func (p *PropRect) enrich() {
	p.WaterTypeLabel = labelOf(WaterPointTypes, p.WaterType)
	p.StatusLabel = labelOf(PropRectStatusLabels, p.Status)
	p.RepairMethodLabel = labelOf(PropRectMethodLabels, p.RepairMethod)
	p.RecheckDateStr = p.RecheckDate.Format("2006-01-02")
	today := time.Now().Truncate(24 * time.Hour)
	if p.RecheckDate.Before(today) {
		switch p.Status {
		case "pending", "rectifying", "rejected":
			p.RectOverdue = true
		case "recheck_pending":
			p.RecheckOverdue = true
		}
	}
	// 复查后统计居民投诉变化（同小区、同地下室类型）
	if p.Status == "verified" || p.Status == "rejected" || p.ComplaintsAfter > 0 {
		p.ComplaintDelta = p.ComplaintsAfter - p.ComplaintsBefore
		down := p.ComplaintsAfter < p.ComplaintsBefore
		p.ComplaintDown = &down
	}
}

// loadReminders 加载督办/超期提醒
func (p *PropRect) loadReminders() {
	p.Reminders = []PropRectReminder{}
	rows, err := db.Query(`SELECT m.id, m.kind, m.content, u.name, m.created_at
		FROM property_rect_reminders m LEFT JOIN users u ON u.id=m.raised_by
		WHERE m.rectification_id=$1 ORDER BY m.id`, p.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var m PropRectReminder
		var name *string
		rows.Scan(&m.ID, &m.Kind, &m.Content, &name, &m.CreatedAt)
		m.KindLabel = labelOf(PropRectReminderKindLabels, m.Kind)
		if name != nil {
			m.RaisedByName = *name
		}
		if m.Kind == "supervise" {
			p.SuperviseCount++
		}
		p.Reminders = append(p.Reminders, m)
	}
}

// ensureOverdueReminders 读取时幂等生成“整改超期/复查超期”提示（街道督办 + 物业负责人）
func ensureOverdueReminders(p *PropRect) {
	add := func(kind, content string) {
		var n int
		db.QueryRow(`SELECT count(*) FROM property_rect_reminders WHERE rectification_id=$1 AND kind=$2`, p.ID, kind).Scan(&n)
		if n == 0 {
			db.Exec(`INSERT INTO property_rect_reminders(rectification_id, kind, content) VALUES($1,$2,$3)`, p.ID, kind, content)
			cacheDel("dashboard:closedloop")
		}
	}
	if p.RectOverdue {
		add("rect_overdue", fmt.Sprintf("整改已超过复查日期 %s，提示街道督办并通知设施责任人 %s 尽快完成排水维修", p.RecheckDateStr, p.FacilityName))
	}
	if p.RecheckOverdue {
		add("recheck_overdue", fmt.Sprintf("物业已于期限前报审，但复查已超过 %s 仍未按图核验，提示街道督办并通知物业负责人 %s", p.RecheckDateStr, p.FacilityName))
	}
}

// ---- 居民投诉变化归因（必须归因到同一积水点） ----
//
// 口径（任务详情 / 闭环看板 / 设施责任人考核共用同一份结果，落库到 complaints_before/after）：
//  1. 优先按关联 water_point_id 归因：经 water_points.report_id 反向找到产生该积水点的投诉；
//  2. 否则按“标准化位置”匹配同一积水点（小写化并去除空白/标点后的等值，或带唯一标识的包含匹配）；
//  3. 只统计地下室/车库类投诉（basement_damp / underground_garage）。
// 因此同小区 A、B 两处各有投诉时，A 任务只计 A；复查后新增的 B 投诉不会改变 A。
//
// 基线窗口 = (建档时间-60天, 建档时间]；当前窗口 = (建档时间, 复查时间]，复查后冻结，
// 之后新增的任何投诉（含同位置）都不再改变已落库的投诉变化，保证可追溯。

// normWaterLoc 与数据库 norm_water_loc(s) 保持同一口径
func normWaterLoc(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case ' ', '\t', '\n', '\r', '　', ',', '，', '.', '。', '、', ';', '；', ':', '：',
			'/', '\\', '(', ')', '（', '）', '-', '—', '_':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// locSamePoint 判断两个位置文本是否指向同一积水点。
// 标准化后等值直接成立；包含匹配仅允许“带数字/字母等唯一标识”的较短串，避免 A区/B区 或泛称互相误匹配。
func locSamePoint(a, b string) bool {
	x, y := normWaterLoc(a), normWaterLoc(b)
	if x == "" || y == "" {
		return false
	}
	if x == y {
		return true
	}
	short, long := x, y
	if len(short) > len(long) {
		short, long = long, short
	}
	hasAlnum := false
	for _, r := range short {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
			hasAlnum = true
			break
		}
	}
	return hasAlnum && len([]rune(short)) >= 3 && strings.Contains(long, short)
}

// attributedComplaintReportIDs 返回时间窗内归因到该积水点的地下室类投诉 ID 集合
func attributedComplaintReportIDs(commID int64, wpID *int64, waterLoc string, since, until time.Time) map[int64]bool {
	out := map[int64]bool{}
	rows, err := db.Query(`SELECT r.id, r.location_desc
		FROM reports r
		WHERE r.community_id=$1 AND r.type IN ('basement_damp','underground_garage')
		  AND r.created_at > $2 AND r.created_at <= $3`, commID, since, until)
	if err != nil {
		return out
	}
	defer rows.Close()

	// 该积水点直接关联的投诉（water_points.report_id）
	linked := map[int64]bool{}
	if wpID != nil {
		lrows, lerr := db.Query(`SELECT report_id FROM water_points WHERE id=$1 AND report_id IS NOT NULL`, *wpID)
		if lerr == nil {
			defer lrows.Close()
			for lrows.Next() {
				var rid sql.NullInt64
				lrows.Scan(&rid)
				if rid.Valid {
					linked[rid.Int64] = true
				}
			}
		}
	}
	for rows.Next() {
		var rid int64
		var loc string
		rows.Scan(&rid, &loc)
		if linked[rid] || locSamePoint(loc, waterLoc) {
			out[rid] = true
		}
	}
	return out
}

func countAttributedComplaints(commID int64, wpID *int64, waterLoc string, since, until time.Time) int {
	return len(attributedComplaintReportIDs(commID, wpID, waterLoc, since, until))
}

func nullIDToPtr(v sql.NullInt64) *int64 {
	if v.Valid {
		id := v.Int64
		return &id
	}
	return nil
}

// baselineWindow / currentWindow 统一窗口
func baselineWindow(createdAt time.Time) (time.Time, time.Time) {
	return createdAt.AddDate(0, 0, -60), createdAt
}

// setComplaintBaseline 建档时写入同积水点投诉基线
func setComplaintBaseline(p *PropRect) {
	since, until := baselineWindow(p.CreatedAt)
	n := countAttributedComplaints(p.CommunityID, p.WaterPointID, p.WaterLocation, since, until)
	db.Exec(`UPDATE property_rectifications SET complaints_before=$1 WHERE id=$2`, n, p.ID)
	p.ComplaintsBefore = n
}

// refreshComplaintCounts 复查时写入“同积水点、建档至复查之间”的当前投诉数（复查后冻结）
func refreshComplaintCounts(p *PropRect) {
	until := time.Now()
	if p.RecheckedAt != nil {
		until = *p.RecheckedAt
	}
	n := countAttributedComplaints(p.CommunityID, p.WaterPointID, p.WaterLocation, p.CreatedAt, until)
	db.Exec(`UPDATE property_rectifications SET complaints_after=$1 WHERE id=$2`, n, p.ID)
	p.ComplaintsAfter = n
}

// backfillPropRectComplaints 历史任务按同一可追溯口径补齐基线/当前投诉数
func backfillPropRectComplaints() {
	rows, err := db.Query(`SELECT id, community_id, water_point_id, water_location, created_at, rechecked_at
		FROM property_rectifications`)
	if err != nil {
		return
	}
	type row struct {
		id, commID  int64
		wpID        sql.NullInt64
		loc         string
		createdAt   time.Time
		recheckedAt sql.NullTime
	}
	var rs []row
	for rows.Next() {
		var x row
		if rows.Scan(&x.id, &x.commID, &x.wpID, &x.loc, &x.createdAt, &x.recheckedAt) != nil {
			continue
		}
		rs = append(rs, x)
	}
	rows.Close()
	for _, x := range rs {
		bSince, bUntil := baselineWindow(x.createdAt)
		wpPtr := nullIDToPtr(x.wpID)
		before := countAttributedComplaints(x.commID, wpPtr, x.loc, bSince, bUntil)
		db.Exec(`UPDATE property_rectifications SET complaints_before=$1 WHERE id=$2`, before, x.id)
		if x.recheckedAt.Valid {
			after := countAttributedComplaints(x.commID, wpPtr, x.loc, x.createdAt, x.recheckedAt.Time)
			db.Exec(`UPDATE property_rectifications SET complaints_after=$1 WHERE id=$2`, after, x.id)
		}
	}
}


// canViewPropRect 只读权限
func canViewPropRect(u *SessionUser, p *PropRect) bool {
	switch u.Role {
	case "street", "supervisor":
		return true
	case "property":
		return (u.CommunityID != nil && *u.CommunityID == p.CommunityID) || u.ID == p.FacilityUserID
	case "grid":
		return u.CommunityID != nil && *u.CommunityID == p.CommunityID
	case "operator":
		if p.WorkOrderID == nil || u.TeamID == nil {
			return false
		}
		var tid int64
		if db.QueryRow(`SELECT team_id FROM work_orders WHERE id=$1`, *p.WorkOrderID).Scan(&tid) == nil {
			return tid == *u.TeamID
		}
	}
	return false
}

// ---------- 自动生成：消杀队对地下室长期积水重复临时处理 ----------

// maybeCreatePropertyRect 在消杀提交后调用：同一地下室积水点第 2 次（或之后）消杀仍只能临时处理时，
// 自动生成物业整改任务，记录排水维修要求、设施责任人、复查日期与居民投诉基线。
func maybeCreatePropertyRect(orderID int64, actor *SessionUser) {
	var commID int64
	var wpID sql.NullInt64
	if err := db.QueryRow(`SELECT community_id, water_point_id FROM work_orders WHERE id=$1`, orderID).Scan(&commID, &wpID); err != nil || !wpID.Valid {
		return
	}
	var wpType, wpLoc string
	if err := db.QueryRow(`SELECT type, location_desc FROM water_points WHERE id=$1`, wpID.Int64).Scan(&wpType, &wpLoc); err != nil {
		return
	}
	if !basementWaterTypes[wpType] {
		return
	}
	// 已有未完成的物业整改任务 → 仅累加临时处理次数，不重复建档
	var existing int64
	var existingStatus string
	err := db.QueryRow(`SELECT id, status FROM property_rectifications WHERE water_point_id=$1 AND status != 'verified' ORDER BY id DESC LIMIT 1`, wpID.Int64).Scan(&existing, &existingStatus)
	if err == nil {
		db.Exec(`UPDATE property_rectifications SET temp_treatment_times=temp_treatment_times+1, updated_at=now() WHERE id=$1`, existing)
		addLog(orderID, actor, "临时处理地下室积水", fmt.Sprintf("消杀队对「%s」再次临时投药/抽排，排水沟长期积水未根治，已记入物业整改任务 #%d 临时处理次数", wpLoc, existing))
		return
	}

	var treatCount int
	db.QueryRow(`SELECT count(*) FROM treatments WHERE work_order_id=$1`, orderID).Scan(&treatCount)
	if treatCount < 2 {
		// 首次处理仍属正常消杀流程，不生成整改；仅当同一积水点需要反复临时处理时认定为设施性积水
		return
	}

	// 设施责任人：本小区物业
	var facilityID int64
	var facilityName string
	if db.QueryRow(`SELECT id, name FROM users WHERE role='property' AND community_id=$1 ORDER BY id LIMIT 1`, commID).Scan(&facilityID, &facilityName) != nil {
		return
	}

	recheckDate := time.Now().AddDate(0, 0, defaultRectDays)
	var id int64
	err = db.QueryRow(`INSERT INTO property_rectifications
		(work_order_id, water_point_id, community_id, water_location, water_type,
		 temp_treated_by, temp_treatment, temp_treated_at, temp_treatment_times,
		 facility_user_id, recheck_date, complaints_before, status, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,now(),$8,$9,$10,$11,'pending',$12) RETURNING id`,
		orderID, wpID, commID, wpLoc, wpType,
		actor.ID, "消杀队临时抽排/投药，抑制蚊幼虫（排水沟长期积水需物业工程维修）", treatCount,
		facilityID, recheckDate, 0, actor.ID).Scan(&id)
	if err != nil {
		return
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, id)
	// 同积水点投诉基线（按 water_point_id / 标准化位置归因）
	if p, e := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id)); e == nil {
		setComplaintBaseline(p)
	}
	ensureParty(orderID, "property", &facilityID, "积水整改设施责任人")
	db.Exec(`UPDATE water_points SET status='rectifying' WHERE id=$1`, wpID.Int64)
	addLog(orderID, actor, "生成物业整改任务",
		fmt.Sprintf("地下室排水沟长期积水，消杀队已临时处理 %d 次仍无法根治，系统自动生成物业整改任务 #%d：设施责任人 %s，复查日期 %s",
			treatCount, id, facilityName, recheckDate.Format("2006-01-02")))
}

// ---------- 列表 / 详情 ----------

func hListPropertyRects(c *Ctx) {
	q := propRectSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	switch c.User.Role {
	case "property":
		q += ` AND (r.facility_user_id=` + next()
		args = append(args, c.User.ID)
		if c.User.CommunityID != nil {
			q += ` OR r.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
		q += `)`
	case "grid":
		if c.User.CommunityID != nil {
			q += ` AND r.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "operator":
		if c.User.TeamID != nil {
			q += ` AND o.team_id=` + next()
			args = append(args, *c.User.TeamID)
		} else {
			jsonOK(c.W, []any{})
			return
		}
	case "resident", "kindergarten":
		jsonOK(c.W, []any{})
		return
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND r.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND r.community_id=` + next()
		args = append(args, v)
	}
	if c.R.URL.Query().Get("overdue") == "1" {
		q += ` AND r.status IN ('pending','rectifying','recheck_pending','rejected') AND r.recheck_date < current_date`
	}
	q += ` ORDER BY (r.status IN ('pending','rectifying','recheck_pending','rejected')) DESC,
		(r.recheck_date < current_date AND r.status != 'verified') DESC, r.recheck_date ASC, r.id DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []PropRect{}
	for rows.Next() {
		p, err := scanPropRect(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		ensureOverdueReminders(p)
		db.QueryRow(`SELECT count(*) FROM property_rect_reminders WHERE rectification_id=$1 AND kind='supervise'`, p.ID).Scan(&p.SuperviseCount)
		list = append(list, *p)
	}
	jsonOK(c.W, list)
}

func hGetPropertyRect(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, err := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "物业整改任务不存在")
		return
	}
	if !canViewPropRect(c.User, p) {
		jsonErr(c.W, 403, "无权查看该整改任务")
		return
	}
	ensureOverdueReminders(p)
	p.loadReminders()
	jsonOK(c.W, p)
}

// ---------- 创建整改任务 ----------

func resolveFacility(commID int64, preferred int64) (int64, error) {
	if preferred != 0 {
		var role string
		var cid *int64
		if err := db.QueryRow(`SELECT role, community_id FROM users WHERE id=$1`, preferred).Scan(&role, &cid); err != nil {
			return 0, fmt.Errorf("设施责任人不存在")
		}
		if role != "property" || cid == nil || *cid != commID {
			return 0, fmt.Errorf("设施责任人须为本小区物业账号")
		}
		return preferred, nil
	}
	var id int64
	if db.QueryRow(`SELECT id FROM users WHERE role='property' AND community_id=$1 ORDER BY id LIMIT 1`, commID).Scan(&id) != nil {
		return 0, fmt.Errorf("该小区暂无物业账号，请先指定设施责任人")
	}
	return id, nil
}

// hCreatePropRectFromOrder 消杀队/网格员临时处理后转物业整改
func hCreatePropRectFromOrder(c *Ctx) {
	orderID, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		FacilityUserID int64  `json:"facility_user_id"`
		RecheckDate    string `json:"recheck_date"`
		RepairDesc     string `json:"repair_desc"`
	}
	decodeBody(c.R, &req)

	var commID int64
	var wpID sql.NullInt64
	var orderStatus string
	if err := db.QueryRow(`SELECT community_id, water_point_id, status FROM work_orders WHERE id=$1`, orderID).Scan(&commID, &wpID, &orderStatus); err != nil {
		jsonErr(c.W, 404, "工单不存在")
		return
	}
	if orderStatus == "closed" {
		jsonErr(c.W, 409, "工单已闭环，无法转物业整改")
		return
	}
	if !wpID.Valid {
		jsonErr(c.W, 400, "该工单未关联积水点，无法生成积水整改任务")
		return
	}
	var wpType, wpLoc string
	db.QueryRow(`SELECT type, location_desc FROM water_points WHERE id=$1`, wpID.Int64).Scan(&wpType, &wpLoc)
	if !basementWaterTypes[wpType] {
		jsonErr(c.W, 400, "仅地下室/地下车库排水沟等设施性积水点可转物业积水整改")
		return
	}
	// 归属校验：消杀队仅本队工单；网格员仅本小区
	if c.User.Role == "operator" {
		var tid int64
		if db.QueryRow(`SELECT team_id FROM work_orders WHERE id=$1`, orderID).Scan(&tid) != nil || c.User.TeamID == nil || tid != *c.User.TeamID {
			jsonErr(c.W, 403, "只能对本消杀队工单转物业整改")
			return
		}
	}
	if c.User.Role == "grid" && c.User.CommunityID != nil && *c.User.CommunityID != commID {
		jsonErr(c.W, 403, "只能对本小区工单转物业整改")
		return
	}
	if _, exists, _ := activePropRect(wpID.Int64); exists {
		jsonErr(c.W, 409, "该积水点已存在未完成的物业整改任务，请勿重复下发")
		return
	}
	var treatCount int
	db.QueryRow(`SELECT count(*) FROM treatments WHERE work_order_id=$1`, orderID).Scan(&treatCount)
	if treatCount < 1 {
		jsonErr(c.W, 409, "请消杀队先到场临时处理（提交消杀记录）后，再转物业整改")
		return
	}
	facilityID, err := resolveFacility(commID, req.FacilityUserID)
	if err != nil {
		jsonErr(c.W, 400, err.Error())
		return
	}
	recheckDate := req.RecheckDate
	if recheckDate == "" {
		recheckDate = time.Now().AddDate(0, 0, defaultRectDays).Format("2006-01-02")
	}

	var id int64
	err = db.QueryRow(`INSERT INTO property_rectifications
		(work_order_id, water_point_id, community_id, water_location, water_type,
		 temp_treated_by, temp_treatment, temp_treated_at, temp_treatment_times,
		 facility_user_id, recheck_date, repair_desc, complaints_before, status, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,now(),$8,$9,$10,$11,$12,'pending',$13) RETURNING id`,
		orderID, wpID, commID, wpLoc, wpType,
		c.User.ID, "消杀队临时抽排/投药，抑制蚊幼虫（排水沟长期积水需物业工程维修）", treatCount,
		facilityID, recheckDate, req.RepairDesc, 0, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, id)
	if p, e := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id)); e == nil {
		setComplaintBaseline(p)
	}
	ensureParty(orderID, "property", &facilityID, "积水整改设施责任人")
	db.Exec(`UPDATE water_points SET status='rectifying' WHERE id=$1`, wpID.Int64)
	addLog(orderID, c.User, "转物业整改", fmt.Sprintf("地下室排水沟长期积水，临时处理 %d 次未根治，下发物业整改任务 #%d，复查日期 %s", treatCount, id, recheckDate))
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]any{"id": id, "rect_no": fmt.Sprintf("PR%08d", id), "message": "已生成物业积水整改任务并通知设施责任人"})
}

func activePropRect(wpID int64) (int64, bool, string) {
	var id int64
	var status string
	if db.QueryRow(`SELECT id, status FROM property_rectifications WHERE water_point_id=$1 AND status != 'verified' ORDER BY id DESC LIMIT 1`, wpID).Scan(&id, &status) != nil {
		return 0, false, ""
	}
	return id, true, status
}

// hCreatePropRect 街道/网格员直接针对地下室积水点建档
func hCreatePropRect(c *Ctx) {
	var req struct {
		CommunityID    int64  `json:"community_id"`
		WaterPointID   int64  `json:"water_point_id"`
		WaterLocation  string `json:"water_location"`
		FacilityUserID int64  `json:"facility_user_id"`
		RecheckDate    string `json:"recheck_date"`
		RepairDesc     string `json:"repair_desc"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.CommunityID == 0 {
		jsonErr(c.W, 400, "请选择小区")
		return
	}
	if c.User.Role == "grid" && c.User.CommunityID != nil {
		req.CommunityID = *c.User.CommunityID
	}
	wpType := "basement_damp"
	var wpID sql.NullInt64
	if req.WaterPointID != 0 {
		var cid int64
		var typ, loc string
		if err := db.QueryRow(`SELECT community_id, type, location_desc FROM water_points WHERE id=$1`, req.WaterPointID).Scan(&cid, &typ, &loc); err != nil {
			jsonErr(c.W, 404, "积水点不存在")
			return
		}
		if cid != req.CommunityID {
			jsonErr(c.W, 400, "积水点与所选小区不一致")
			return
		}
		wpID = sql.NullInt64{Int64: req.WaterPointID, Valid: true}
		if !basementWaterTypes[typ] {
			jsonErr(c.W, 400, "仅地下室/地下车库排水沟等设施性积水点可建档物业积水整改")
			return
		}
		wpType = typ
		if req.WaterLocation == "" {
			req.WaterLocation = loc
		}
		if _, exists, _ := activePropRect(req.WaterPointID); exists {
			jsonErr(c.W, 409, "该积水点已存在未完成的物业整改任务")
			return
		}
	}
	if req.WaterLocation == "" {
		jsonErr(c.W, 400, "请填写积水点位置（如：地下车库 B2 层排水沟）")
		return
	}
	facilityID, err := resolveFacility(req.CommunityID, req.FacilityUserID)
	if err != nil {
		jsonErr(c.W, 400, err.Error())
		return
	}
	if req.RecheckDate == "" {
		req.RecheckDate = time.Now().AddDate(0, 0, defaultRectDays).Format("2006-01-02")
	}
	var id int64
	err = db.QueryRow(`INSERT INTO property_rectifications
		(work_order_id, water_point_id, community_id, water_location, water_type,
		 facility_user_id, recheck_date, repair_desc, complaints_before, status, created_by)
		VALUES(NULL,$1,$2,$3,$4,$5,$6,$7,$8,'pending',$9) RETURNING id`,
		wpID, req.CommunityID, req.WaterLocation, wpType, facilityID, req.RecheckDate, req.RepairDesc, 0, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, id)
	if p, e := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id)); e == nil {
		setComplaintBaseline(p)
	}
	if wpID.Valid {
		db.Exec(`UPDATE water_points SET status='rectifying' WHERE id=$1`, wpID.Int64)
	}
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]any{"id": id, "rect_no": fmt.Sprintf("PR%08d", id), "message": "物业积水整改任务已建档下发"})
}

// ---------- 物业整改流转 ----------

func loadPropRectForMutation(c *Ctx, id int64, facilityOnly bool) (*PropRect, bool) {
	p, err := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "整改任务不存在")
		return nil, false
	}
	if facilityOnly && p.FacilityUserID != c.User.ID {
		jsonErr(c.W, 403, "仅该任务的设施责任人可执行此操作")
		return nil, false
	}
	if !facilityOnly && !canViewPropRect(c.User, p) {
		jsonErr(c.W, 403, "无权操作该整改任务")
		return nil, false
	}
	return p, true
}

func hStartPropRect(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, ok := loadPropRectForMutation(c, id, true)
	if !ok {
		return
	}
	if p.Status != "pending" && p.Status != "rejected" {
		jsonErr(c.W, 409, "仅待整改/返工状态的任务可受理")
		return
	}
	db.Exec(`UPDATE property_rectifications SET status='rectifying', updated_at=now() WHERE id=$1`, id)
	if p.WorkOrderID != nil {
		addLog(*p.WorkOrderID, c.User, "物业受理积水整改", fmt.Sprintf("设施责任人 %s 进场维修排水设施，复查日期 %s", p.FacilityName, p.RecheckDateStr))
	}
	jsonOK(c.W, map[string]string{"message": "已受理，整改中"})
}

func hCompletePropRect(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		RepairDesc   string   `json:"repair_desc"`
		RepairMethod string   `json:"repair_method"`
		Photos       []string `json:"rectify_photos"`
		PhotoRemark  string   `json:"rectify_photo_remark"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	p, ok := loadPropRectForMutation(c, id, true)
	if !ok {
		return
	}
	if p.Status != "pending" && p.Status != "rectifying" && p.Status != "rejected" {
		jsonErr(c.W, 409, "当前状态不允许提交整改完成")
		return
	}
	if req.RepairDesc == "" {
		jsonErr(c.W, 400, "请填写排水维修情况说明")
		return
	}
	if _, ok := PropRectMethodLabels[req.RepairMethod]; !ok {
		jsonErr(c.W, 400, "请选择排水处理方式")
		return
	}
	photos := StringList(req.Photos)
	if len(photos) == 0 {
		jsonErr(c.W, 400, "请上传整改照片（须能看清积水点位置与处理方式）")
		return
	}
	if req.PhotoRemark == "" {
		jsonErr(c.W, 400, "请在照片说明中标明积水点位置与处理方式，供复查人员按图核验")
		return
	}
	db.Exec(`UPDATE property_rectifications
		SET status='recheck_pending', repair_desc=$1, repair_method=$2, rectify_photos=$3, rectify_photo_remark=$4,
		    rectified_by=$5, rectified_at=now(), updated_at=now()
		WHERE id=$6`, req.RepairDesc, req.RepairMethod, photos, req.PhotoRemark, c.User.ID, id)
	if p.WorkOrderID != nil {
		addLog(*p.WorkOrderID, c.User, "物业完成排水整改",
			fmt.Sprintf("处理方式【%s】%s；整改照片已标明积水点位置与处理方式，待复查人员按图核验", labelOf(PropRectMethodLabels, req.RepairMethod), req.RepairDesc))
	}
	cacheDel("dashboard:closedloop")
	jsonOK(c.W, map[string]string{"message": "整改完成，已提交复查（照片须标明位置与处理方式）"})
}

func hRecheckPropRect(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Pass   bool     `json:"pass"`
		Photos []string `json:"recheck_photos"`
		Remark string   `json:"recheck_remark"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	p, ok := loadPropRectForMutation(c, id, false)
	if !ok {
		return
	}
	if p.Status != "recheck_pending" {
		jsonErr(c.W, 409, "仅待复查状态的任务可复查核验")
		return
	}
	if len(req.Photos) == 0 {
		jsonErr(c.W, 400, "请上传复查照片（对照整改照片按图核验）")
		return
	}
	if req.Remark == "" {
		req.Remark = "复查人员已对照整改照片按图核验"
	}
	result := "pass"
	newStatus := "verified"
	if !req.Pass {
		result = "fail"
		newStatus = "rejected"
	}
	refreshComplaintCounts(p)
	db.Exec(`UPDATE property_rectifications
		SET status=$1, recheck_photos=$2, recheck_remark=$3, recheck_result=$4, rechecked_by=$5, rechecked_at=now(),
		    complaints_after=$6, updated_at=now()
		WHERE id=$7`, newStatus, StringList(req.Photos), req.Remark, result, c.User.ID, p.ComplaintsAfter, id)

	if req.Pass {
		if p.WaterPointID != nil {
			db.Exec(`UPDATE water_points SET status='cleared', last_recheck_at=now() WHERE id=$1`, *p.WaterPointID)
		}
		if p.WorkOrderID != nil {
			addLog(*p.WorkOrderID, c.User, "积水整改复查通过",
				fmt.Sprintf("复查人员按图核验通过，排水沟已无积水；居民投诉由 %d 起变为 %d 起", p.ComplaintsBefore, p.ComplaintsAfter))
		}
		jsonOK(c.W, map[string]any{"message": "复查通过，积水点已清除", "complaints_before": p.ComplaintsBefore, "complaints_after": p.ComplaintsAfter})
	} else {
		db.Exec(`INSERT INTO property_rect_reminders(rectification_id, kind, content, raised_by)
			VALUES($1,'supervise',$2,$3)`, id, "复查不通过，设施需返工："+req.Remark, c.User.ID)
		if p.WorkOrderID != nil {
			addLog(*p.WorkOrderID, c.User, "积水整改复查不通过", req.Remark+"（退回物业返工，街道跟进督办）")
		}
		jsonOK(c.W, map[string]string{"message": "复查不通过，已退回设施责任人返工并提示街道督办"})
	}
	cacheDel("dashboard:closedloop")
}

func hSupervisePropRect(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Content == "" {
		jsonErr(c.W, 400, "请填写督办意见")
		return
	}
	p, err := scanPropRect(db.QueryRow(propRectSelect+` WHERE r.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "整改任务不存在")
		return
	}
	if p.Status == "verified" {
		jsonErr(c.W, 409, "任务已复查通过，无需督办")
		return
	}
	db.Exec(`INSERT INTO property_rect_reminders(rectification_id, kind, content, raised_by) VALUES($1,'supervise',$2,$3)`, id, req.Content, c.User.ID)
	if p.WorkOrderID != nil {
		addLog(*p.WorkOrderID, c.User, "街道督办积水整改", fmt.Sprintf("向设施责任人 %s 发出督办：%s", p.FacilityName, req.Content))
	}
	jsonOK(c.W, map[string]string{"message": "督办意见已记录，并提示物业设施责任人"})
}
