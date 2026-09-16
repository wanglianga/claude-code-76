package main

import (
	"time"
)

func registerDashboardRoutes() {
	handle("GET /api/risk-periods", hListRiskPeriods)
	handle("POST /api/risk-periods", hCreateRiskPeriod, "street")
	handle("POST /api/risk-periods/{id}/activate", hActivateRiskPeriod, "street")
	handle("POST /api/risk-periods/{id}/deactivate", hDeactivateRiskPeriod, "street")
	handle("GET /api/dashboard/closed-loop", hClosedLoopDashboard, "street", "supervisor")
	handle("GET /api/dashboard/assessment", hAssessment, "street", "supervisor")
	handle("GET /api/dashboard/complaint-trend", hComplaintTrend, "street", "supervisor")
}

// ---------- 重点风险期 ----------

type RiskPeriod struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	Disease             string `json:"disease"`
	StartDate           string `json:"start_date"`
	EndDate             string `json:"end_date"`
	RecheckIntervalDays int    `json:"recheck_interval_days"`
	Active              bool   `json:"active"`
}

// activeRiskPeriod 当前生效的重点风险期（Redis 缓存 60s）
func activeRiskPeriod() *RiskPeriod {
	var rp RiskPeriod
	if cacheGet("risk:active", &rp) {
		if rp.ID == 0 {
			return nil
		}
		return &rp
	}
	err := db.QueryRow(`SELECT id, name, disease, to_char(start_date,'YYYY-MM-DD'), to_char(end_date,'YYYY-MM-DD'), recheck_interval_days, active
		FROM risk_periods WHERE active=true AND start_date <= current_date AND end_date >= current_date
		ORDER BY id DESC LIMIT 1`).
		Scan(&rp.ID, &rp.Name, &rp.Disease, &rp.StartDate, &rp.EndDate, &rp.RecheckIntervalDays, &rp.Active)
	if err != nil {
		cacheSet("risk:active", RiskPeriod{}, 60*time.Second)
		return nil
	}
	cacheSet("risk:active", rp, 60*time.Second)
	return &rp
}

func hListRiskPeriods(c *Ctx) {
	rows, err := db.Query(`SELECT id, name, disease, to_char(start_date,'YYYY-MM-DD'), to_char(end_date,'YYYY-MM-DD'), recheck_interval_days, active
		FROM risk_periods ORDER BY id DESC`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []RiskPeriod{}
	for rows.Next() {
		var rp RiskPeriod
		rows.Scan(&rp.ID, &rp.Name, &rp.Disease, &rp.StartDate, &rp.EndDate, &rp.RecheckIntervalDays, &rp.Active)
		list = append(list, rp)
	}
	jsonOK(c.W, map[string]any{"current": activeRiskPeriod(), "list": list})
}

func hCreateRiskPeriod(c *Ctx) {
	var req struct {
		Name                string `json:"name"`
		Disease             string `json:"disease"`
		StartDate           string `json:"start_date"`
		EndDate             string `json:"end_date"`
		RecheckIntervalDays int    `json:"recheck_interval_days"`
		Active              bool   `json:"active"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Name == "" || req.StartDate == "" || req.EndDate == "" {
		jsonErr(c.W, 400, "请填写风险期名称与起止日期")
		return
	}
	if req.Disease == "" {
		req.Disease = "登革热"
	}
	if req.RecheckIntervalDays <= 0 {
		req.RecheckIntervalDays = 3
	}
	if req.Active {
		db.Exec(`UPDATE risk_periods SET active=false`)
	}
	var id int64
	err := db.QueryRow(`INSERT INTO risk_periods(name, disease, start_date, end_date, recheck_interval_days, active, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		req.Name, req.Disease, req.StartDate, req.EndDate, req.RecheckIntervalDays, req.Active, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if req.Active {
		refreshKeyWaterPoints()
	}
	cacheDel("risk:active")
	jsonOK(c.W, map[string]any{"id": id, "message": "重点风险期已保存"})
}

func hActivateRiskPeriod(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	db.Exec(`UPDATE risk_periods SET active=false`)
	res, err := db.Exec(`UPDATE risk_periods SET active=true WHERE id=$1`, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 404, "风险期不存在")
		return
	}
	refreshKeyWaterPoints()
	cacheDel("risk:active")
	jsonOK(c.W, map[string]string{"message": "重点风险期已开启：复查频次提高，重点积水点清单已自动更新"})
}

func hDeactivateRiskPeriod(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	db.Exec(`UPDATE risk_periods SET active=false WHERE id=$1`, id)
	cacheDel("risk:active")
	jsonOK(c.W, map[string]string{"message": "重点风险期已结束"})
}

// ---------- 闭环进度看板 ----------

func hClosedLoopDashboard(c *Ctx) {
	type Row struct {
		CommunityID        int64   `json:"community_id"`
		CommunityName      string  `json:"community_name"`
		ReportsTotal       int     `json:"reports_total"`
		ReportsPending     int     `json:"reports_pending"`
		OrdersTotal        int     `json:"orders_total"`
		OrdersActive       int     `json:"orders_active"`
		OrdersTreated      int     `json:"orders_treated"`
		OrdersRechecked    int     `json:"orders_rechecked"`
		RecheckPass        int     `json:"recheck_pass"`
		RecheckOverdue     int     `json:"recheck_overdue"`
		RectOpen           int     `json:"rectifications_open"`
		RectVerified       int     `json:"rectifications_verified"`
		PropRectTotal      int     `json:"prop_rect_total"`
		PropRectOpen       int     `json:"prop_rect_open"`
		PropRectOverdue    int     `json:"prop_rect_overdue"`
		PropRectVerified   int     `json:"prop_rect_verified"`
		PropRectCmpBefore  int     `json:"prop_rect_complaints_before"`
		PropRectCmpAfter   int     `json:"prop_rect_complaints_after"`
		OrdersClosed       int     `json:"orders_closed"`
		CloseRate          float64 `json:"close_rate"`
		KeyWaterPoints     int     `json:"key_water_points"`
		OpenWaterPoints    int     `json:"open_water_points"`
		ComplaintsThisMonth int    `json:"complaints_this_month"`
		ComplaintsLastMonth int    `json:"complaints_last_month"`
		ComplaintDeclinePct float64 `json:"complaint_decline_pct"`
	}
	rows, err := db.Query(`
		SELECT cm.id, cm.name,
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id),
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.status='pending'),
		  (SELECT count(*) FROM work_orders o WHERE o.community_id=cm.id),
		  (SELECT count(*) FROM work_orders o WHERE o.community_id=cm.id AND o.status != 'closed'),
		  (SELECT count(DISTINCT t.work_order_id) FROM treatments t JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=cm.id),
		  (SELECT count(DISTINCT rc.work_order_id) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.community_id=cm.id),
		  (SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.community_id=cm.id AND rc.result='pass'),
		  (SELECT count(*) FROM work_orders o WHERE o.community_id=cm.id AND o.status='recheck_pending' AND o.recheck_due_at < now()),
		  (SELECT count(*) FROM rectifications rt JOIN work_orders o ON o.id=rt.work_order_id WHERE o.community_id=cm.id AND rt.status IN ('pending','in_progress','done')),
		  (SELECT count(*) FROM rectifications rt JOIN work_orders o ON o.id=rt.work_order_id WHERE o.community_id=cm.id AND rt.status='verified'),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.status != 'verified'),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.status != 'verified' AND pr.recheck_date < current_date),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.status='verified'),
		  (SELECT COALESCE(sum(pr.complaints_before),0) FROM property_rectifications pr WHERE pr.community_id=cm.id),
		  (SELECT COALESCE(sum(pr.complaints_after),0) FROM property_rectifications pr WHERE pr.community_id=cm.id),
		  (SELECT count(*) FROM work_orders o WHERE o.community_id=cm.id AND o.status='closed'),
		  (SELECT count(*) FROM water_points w WHERE w.community_id=cm.id AND w.is_key AND w.status != 'cleared'),
		  (SELECT count(*) FROM water_points w WHERE w.community_id=cm.id AND w.status != 'cleared'),
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.created_at >= date_trunc('month', now())),
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.created_at >= date_trunc('month', now()) - interval '1 month' AND r.created_at < date_trunc('month', now()))
		FROM communities cm ORDER BY cm.id`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []Row{}
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.CommunityID, &r.CommunityName, &r.ReportsTotal, &r.ReportsPending,
			&r.OrdersTotal, &r.OrdersActive, &r.OrdersTreated, &r.OrdersRechecked, &r.RecheckPass,
			&r.RecheckOverdue, &r.RectOpen, &r.RectVerified,
			&r.PropRectTotal, &r.PropRectOpen, &r.PropRectOverdue, &r.PropRectVerified,
			&r.PropRectCmpBefore, &r.PropRectCmpAfter,
			&r.OrdersClosed,
			&r.KeyWaterPoints, &r.OpenWaterPoints, &r.ComplaintsThisMonth, &r.ComplaintsLastMonth); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		if r.OrdersTotal > 0 {
			r.CloseRate = float64(r.OrdersClosed) / float64(r.OrdersTotal) * 100
		}
		if r.ComplaintsLastMonth > 0 {
			r.ComplaintDeclinePct = float64(r.ComplaintsLastMonth-r.ComplaintsThisMonth) / float64(r.ComplaintsLastMonth) * 100
		}
		list = append(list, r)
	}
	jsonOK(c.W, map[string]any{
		"risk_period":           activeRiskPeriod(),
		"recheck_interval_days": recheckIntervalDays(),
		"rows":                  list,
	})
}

// ---------- 街道考核 ----------

func hAssessment(c *Ctx) {
	month := c.R.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	start := month + "-01"

	type CommRow struct {
		CommunityID         int64    `json:"community_id"`
		CommunityName       string   `json:"community_name"`
		Complaints          int      `json:"complaints"`
		ComplaintsPrev      int      `json:"complaints_prev"`
		DeclinePct          float64  `json:"complaint_decline_pct"`
		OrdersClosed        int      `json:"orders_closed"`
		AvgCloseHours       *float64 `json:"avg_close_hours"`
		RecheckTotal        int      `json:"recheck_total"`
		RecheckPass         int      `json:"recheck_pass"`
		RecheckPassRate     float64  `json:"recheck_pass_rate"`
		ChemicalUsed        float64  `json:"chemical_used"`
		PropertyIssues      int      `json:"property_facility_issues"`
		RectTotal           int      `json:"rectifications_total"`
		RectVerified        int      `json:"rectifications_verified"`
		PropRectTotal       int      `json:"prop_rect_total"`
		PropRectOverdue     int      `json:"prop_rect_overdue"`
		PropRectVerified    int      `json:"prop_rect_verified"`
		PropRectRecheckFail int      `json:"prop_rect_recheck_fail"`
		AccessRefused       int      `json:"access_refused"`
		AccessRiskContinued int      `json:"access_risk_continued"`
		AccessCompleted     int      `json:"access_completed"`
	}
	rows, err := db.Query(`
		SELECT cm.id, cm.name,
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.created_at >= $1::date AND r.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM reports r WHERE r.community_id=cm.id AND r.created_at >= ($1::date - interval '1 month') AND r.created_at < $1::date),
		  (SELECT count(*) FROM work_orders o WHERE o.community_id=cm.id AND o.closed_at >= $1::date AND o.closed_at < ($1::date + interval '1 month')),
		  (SELECT avg(extract(epoch from (o.closed_at - o.created_at))/3600.0) FROM work_orders o WHERE o.community_id=cm.id AND o.closed_at >= $1::date AND o.closed_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.community_id=cm.id AND rc.created_at >= $1::date AND rc.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.community_id=cm.id AND rc.result='pass' AND rc.created_at >= $1::date AND rc.created_at < ($1::date + interval '1 month')),
		  (SELECT COALESCE(sum(t.chemical_used),0) FROM treatments t JOIN work_orders o ON o.id=t.work_order_id WHERE o.community_id=cm.id AND t.created_at >= $1::date AND t.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM issues i JOIN work_orders o ON o.id=i.work_order_id WHERE o.community_id=cm.id AND i.type='property_facility' AND i.created_at >= $1::date AND i.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rectifications rt JOIN work_orders o ON o.id=rt.work_order_id WHERE o.community_id=cm.id AND rt.created_at >= $1::date AND rt.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rectifications rt JOIN work_orders o ON o.id=rt.work_order_id WHERE o.community_id=cm.id AND rt.status='verified' AND rt.verified_at >= $1::date AND rt.verified_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.created_at >= $1::date AND pr.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.created_at >= $1::date AND pr.created_at < ($1::date + interval '1 month') AND pr.recheck_date < current_date AND pr.status != 'verified'),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.status='verified' AND pr.rechecked_at >= $1::date AND pr.rechecked_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM property_rectifications pr WHERE pr.community_id=cm.id AND pr.status='rejected' AND pr.updated_at >= $1::date AND pr.updated_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM access_cases ac WHERE ac.community_id=cm.id AND ac.created_at >= $1::date AND ac.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM access_cases ac WHERE ac.community_id=cm.id AND ac.risk_continued AND ac.updated_at >= $1::date AND ac.updated_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM access_cases ac WHERE ac.community_id=cm.id AND ac.status='completed' AND ac.completed_at >= $1::date AND ac.completed_at < ($1::date + interval '1 month'))
		FROM communities cm ORDER BY cm.id`, start)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	commRows := []CommRow{}
	for rows.Next() {
		var r CommRow
		if err := rows.Scan(&r.CommunityID, &r.CommunityName, &r.Complaints, &r.ComplaintsPrev,
			&r.OrdersClosed, &r.AvgCloseHours, &r.RecheckTotal, &r.RecheckPass, &r.ChemicalUsed,
			&r.PropertyIssues, &r.RectTotal, &r.RectVerified,
			&r.PropRectTotal, &r.PropRectOverdue, &r.PropRectVerified, &r.PropRectRecheckFail,
			&r.AccessRefused, &r.AccessRiskContinued, &r.AccessCompleted); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		if r.ComplaintsPrev > 0 {
			r.DeclinePct = float64(r.ComplaintsPrev-r.Complaints) / float64(r.ComplaintsPrev) * 100
		}
		if r.RecheckTotal > 0 {
			r.RecheckPassRate = float64(r.RecheckPass) / float64(r.RecheckTotal) * 100
		}
		commRows = append(commRows, r)
	}

	type TeamRow struct {
		TeamID         int64   `json:"team_id"`
		TeamName       string  `json:"team_name"`
		OrdersAssigned int     `json:"orders_assigned"`
		Treatments     int     `json:"treatments"`
		ChemicalUsed   float64 `json:"chemical_used"`
		RecheckPass    int     `json:"recheck_pass"`
		RecheckTotal   int     `json:"recheck_total"`
		PassRate       float64 `json:"recheck_pass_rate"`
	}
	trows, err := db.Query(`
		SELECT t.id, t.name,
		  (SELECT count(*) FROM work_orders o WHERE o.team_id=t.id AND o.created_at >= $1::date AND o.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM treatments tr JOIN work_orders o ON o.id=tr.work_order_id WHERE o.team_id=t.id AND tr.created_at >= $1::date AND tr.created_at < ($1::date + interval '1 month')),
		  (SELECT COALESCE(sum(tr.chemical_used),0) FROM treatments tr JOIN work_orders o ON o.id=tr.work_order_id WHERE o.team_id=t.id AND tr.created_at >= $1::date AND tr.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.team_id=t.id AND rc.result='pass' AND rc.created_at >= $1::date AND rc.created_at < ($1::date + interval '1 month')),
		  (SELECT count(*) FROM rechecks rc JOIN work_orders o ON o.id=rc.work_order_id WHERE o.team_id=t.id AND rc.created_at >= $1::date AND rc.created_at < ($1::date + interval '1 month'))
		FROM teams t ORDER BY t.id`, start)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer trows.Close()
	teamRows := []TeamRow{}
	for trows.Next() {
		var r TeamRow
		if err := trows.Scan(&r.TeamID, &r.TeamName, &r.OrdersAssigned, &r.Treatments, &r.ChemicalUsed, &r.RecheckPass, &r.RecheckTotal); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		if r.RecheckTotal > 0 {
			r.PassRate = float64(r.RecheckPass) / float64(r.RecheckTotal) * 100
		}
		teamRows = append(teamRows, r)
	}

	// 物业设施责任人维度：积水整改任务数、超期数（影响物业考核）、复查合格率、督办次数、居民投诉变化
	type FacilityRow struct {
		UserID          int64   `json:"user_id"`
		UserName        string  `json:"user_name"`
		CommunityName   string  `json:"community_name"`
		RectTotal       int     `json:"rect_total"`
		RectOverdue     int     `json:"rect_overdue"`
		RectVerified    int     `json:"rect_verified"`
		RecheckFail     int     `json:"recheck_fail"`
		RecheckPassRate float64 `json:"recheck_pass_rate"`
		SuperviseCount  int     `json:"supervise_count"`
		ComplaintsBefore int    `json:"complaints_before"`
		ComplaintsAfter int     `json:"complaints_after"`
		ComplaintDelta  int     `json:"complaint_delta"`
	}
	frows, err := db.Query(`
		SELECT u.id, u.name, cm.name,
		  count(*) AS total,
		  count(*) FILTER (WHERE pr.status != 'verified' AND pr.recheck_date < current_date) AS overdue,
		  count(*) FILTER (WHERE pr.status='verified') AS verified,
		  count(*) FILTER (WHERE pr.status='rejected') AS rejected,
		  COALESCE((SELECT count(*) FROM property_rect_reminders m WHERE m.rectification_id=pr.id AND m.kind='supervise'),0) AS supervise,
		  COALESCE(sum(pr.complaints_before),0), COALESCE(sum(pr.complaints_after),0)
		FROM property_rectifications pr
		JOIN users u ON u.id=pr.facility_user_id
		JOIN communities cm ON cm.id=pr.community_id
		WHERE pr.created_at >= $1::date AND pr.created_at < ($1::date + interval '1 month')
		GROUP BY u.id, u.name, cm.name, pr.id`, start)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer frows.Close()
	facAgg := map[int64]*FacilityRow{}
	var order []int64
	for frows.Next() {
		var uid int64
		var uname, cname string
		var total, overdue, verified, rejected, supervise, before, after int
		if frows.Scan(&uid, &uname, &cname, &total, &overdue, &verified, &rejected, &supervise, &before, &after) != nil {
			continue
		}
		a, ok := facAgg[uid]
		if !ok {
			a = &FacilityRow{UserID: uid, UserName: uname, CommunityName: cname}
			facAgg[uid] = a
			order = append(order, uid)
		}
		a.RectTotal += total
		a.RectOverdue += overdue
		a.RectVerified += verified
		a.RecheckFail += rejected
		a.SuperviseCount += supervise
		a.ComplaintsBefore += before
		a.ComplaintsAfter += after
	}
	facilityRows := []FacilityRow{}
	for _, uid := range order {
		a := facAgg[uid]
		finished := a.RectVerified + a.RecheckFail
		if finished > 0 {
			a.RecheckPassRate = float64(a.RectVerified) / float64(finished) * 100
		}
		a.ComplaintDelta = a.ComplaintsAfter - a.ComplaintsBefore
		facilityRows = append(facilityRows, *a)
	}

	jsonOK(c.W, map[string]any{
		"month":       month,
		"communities": commRows,
		"teams":       teamRows,
		"facilities":  facilityRows,
	})
}

// ---------- 投诉趋势 ----------

func hComplaintTrend(c *Ctx) {
	rows, err := db.Query(`
		SELECT to_char(r.created_at, 'YYYY-MM') AS m, cm.name, count(*)
		FROM reports r JOIN communities cm ON cm.id=r.community_id
		WHERE r.created_at > now() - interval '6 months'
		GROUP BY m, cm.name ORDER BY m, cm.name`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type Point struct {
		Month     string `json:"month"`
		Community string `json:"community"`
		Count     int    `json:"count"`
	}
	list := []Point{}
	for rows.Next() {
		var p Point
		rows.Scan(&p.Month, &p.Community, &p.Count)
		list = append(list, p)
	}
	jsonOK(c.W, list)
}
