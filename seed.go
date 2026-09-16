package main

import (
	"encoding/json"
	"log"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// seed 仅在 users 表为空时执行，保证幂等
func seed() {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n > 0 {
		return
	}
	log.Println("seeding demo data...")

	hash := func(pwd string) string {
		b, _ := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		return string(b)
	}

	communities := []struct{ name, addr string }{
		{"阳光小区", "幸福路 12 号"},
		{"滨江花园", "滨江大道 88 号"},
		{"老城厢社区", "文庙街 3 号"},
	}
	commIDs := map[string]int64{}
	for _, c := range communities {
		var id int64
		if err := db.QueryRow(`INSERT INTO communities(name, address) VALUES($1,$2) RETURNING id`, c.name, c.addr).Scan(&id); err != nil {
			log.Fatalf("seed communities: %v", err)
		}
		commIDs[c.name] = id
	}

	teamIDs := map[string]int64{}
	for _, t := range []struct{ name, desc string }{
		{"消杀一队", "负责阳光小区、老城厢社区"},
		{"消杀二队", "负责滨江花园及周边"},
	} {
		var id int64
		if err := db.QueryRow(`INSERT INTO teams(name, description) VALUES($1,$2) RETURNING id`, t.name, t.desc).Scan(&id); err != nil {
			log.Fatalf("seed teams: %v", err)
		}
		teamIDs[t.name] = id
	}

	yang, bin, lao := commIDs["阳光小区"], commIDs["滨江花园"], commIDs["老城厢社区"]
	t1, t2 := teamIDs["消杀一队"], teamIDs["消杀二队"]

	type u struct {
		username, pwd, name, role, phone string
		commID, teamID                   *int64
	}
	users := []u{
		{"resident", "Resident@123", "张伟", "resident", "13800000001", &yang, nil},
		{"resident2", "Resident@123", "李芳", "resident", "13800000002", &bin, nil},
		{"property", "Property@123", "王强", "property", "13800000003", &yang, nil},
		{"property2", "Property@123", "赵敏", "property", "13800000004", &bin, nil},
		{"grid", "Grid@123", "陈静", "grid", "13800000005", &yang, nil},
		{"operator", "Operator@123", "刘洋", "operator", "13800000006", nil, &t1},
		{"operator2", "Operator@123", "孙磊", "operator", "13800000007", nil, &t2},
		{"street", "Street@123", "周主任", "street", "13800000008", nil, nil},
		{"supervisor", "Supervisor@123", "吴监督", "supervisor", "13800000009", nil, nil},
		{"kindergarten", "Kindergarten@123", "王园", "kindergarten", "13911110001", &yang, nil},
		{"kindergarten2", "Kindergarten@123", "李园长", "kindergarten", "13911110002", &bin, nil},
	}
	userIDs := map[string]int64{}
	for _, x := range users {
		var id int64
		if err := db.QueryRow(`INSERT INTO users(username, password_hash, name, role, phone, community_id, team_id)
			VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			x.username, hash(x.pwd), x.name, x.role, x.phone, x.commID, x.teamID).Scan(&id); err != nil {
			log.Fatalf("seed users: %v", err)
		}
		userIDs[x.username] = id
	}

	chems := []struct {
		name, unit string
		stock, safe float64
	}{
		{"高效氯氟氰菊酯", "升", 100, 20},
		{"苏云金杆菌(BTI)", "公斤", 50, 10},
		{"倍硫磷", "升", 30, 10},
	}
	for _, ch := range chems {
		if _, err := db.Exec(`INSERT INTO chemicals(name, unit, stock, safe_stock) VALUES($1,$2,$3,$4)`, ch.name, ch.unit, ch.stock, ch.safe); err != nil {
			log.Fatalf("seed chemicals: %v", err)
		}
	}

	// 排班：今天起连续 3 天，两队全天各 5 单
	for d := 0; d < 3; d++ {
		day := time.Now().AddDate(0, 0, d).Format("2006-01-02")
		for _, tid := range []int64{t1, t2} {
			if _, err := db.Exec(`INSERT INTO team_schedules(team_id, work_date, shift, max_orders) VALUES($1,$2,'allday',5)`, tid, day); err != nil {
				log.Fatalf("seed schedules: %v", err)
			}
		}
	}

	// 近 6 天降雨（毫米）
	rain := map[int64][]float64{
		yang: {12, 18, 5, 0, 22, 8},
		bin:  {5, 3, 0, 0, 8, 2},
		lao:  {30, 25, 10, 0, 5, 12},
	}
	for cid, arr := range rain {
		for i, mm := range arr {
			day := time.Now().AddDate(0, 0, -(6 - i)).Format("2006-01-02")
			if _, err := db.Exec(`INSERT INTO rainfall_records(community_id, rain_date, amount_mm) VALUES($1,$2,$3)`, cid, day, mm); err != nil {
				log.Fatalf("seed rainfall: %v", err)
			}
		}
	}

	// 登革热重点风险期（当前生效）
	if _, err := db.Exec(`INSERT INTO risk_periods(name, disease, start_date, end_date, recheck_interval_days, active, created_by)
		VALUES('登革热重点防控期','登革热',$1,$2,3,true,$3)`,
		time.Now().AddDate(0, 0, -14).Format("2006-01-02"),
		time.Now().AddDate(0, 0, 16).Format("2006-01-02"),
		userIDs["street"]); err != nil {
		log.Fatalf("seed risk period: %v", err)
	}

	// 演示上报（含上月数据，便于投诉趋势展示）
	type r struct {
		reporter  string
		comm      int64
		typ       string
		loc       string
		pop       string
		pets      bool
		desc      string
		daysAgo   int
	}
	reports := []r{
		{"resident", yang, "mosquito_dense", "3 号楼绿化带旁", "老人儿童居多", true, "傍晚蚊虫成群，多名居民被叮咬", 2},
		{"resident", yang, "child_bite", "小区儿童游乐场", "儿童活动密集", false, "孩子在游乐场玩耍后腿部多处红肿", 1},
		{"grid", yang, "rooftop_water", "7 号楼楼顶", "顶楼住户", false, "楼顶排水口堵塞，雨后积水已 3 天", 10},
		{"property", yang, "basement_damp", "地下车库 B2 层", "车主", false, "B2 层墙角长期潮湿，集水井有异味", 35},
		{"resident2", bin, "greenbelt_water", "中央绿化带", "散步居民较多", true, "绿化带低洼处积水，孳生蚊虫", 3},
		{"resident2", bin, "trash_odor", "东门垃圾站", "过往行人", false, "垃圾桶满溢异味明显，蝇虫聚集", 25},
		{"grid", lao, "mosquito_dense", "旧改废弃工地", "周边老旧居民楼", false, "工地基坑积水，夜间蚊虫密集", 5},
		{"property2", bin, "rooftop_water", "2 栋楼顶水箱旁", "顶楼住户", false, "水箱检修后周边积水未清理", 45},
	}
	for _, x := range reports {
		var id int64
		err := db.QueryRow(`INSERT INTO reports(report_no, reporter_id, community_id, type, location_desc, nearby_population, has_pets, description, created_at)
			VALUES('', $1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			userIDs[x.reporter], x.comm, x.typ, x.loc, x.pop, x.pets, x.desc,
			time.Now().AddDate(0, 0, -x.daysAgo)).Scan(&id)
		if err != nil {
			log.Fatalf("seed reports: %v", err)
		}
		if _, err := db.Exec(`UPDATE reports SET report_no='RPT'||LPAD(id::text,8,'0') WHERE id=$1`, id); err != nil {
			log.Fatalf("seed report no: %v", err)
		}
	}

	// 演示积水点（覆盖六类蚊虫来源）
	type wp struct {
		comm    int64
		typ     string
		loc     string
		larvae  bool
		daysAgo int
	}
	points := []wp{
		{yang, "rooftop_tank", "7 号楼楼顶水箱", false, 20},
		{yang, "waste_tire", "北门废旧轮胎堆放点", false, 15},
		{yang, "underground_garage", "地下车库 B2 集水井", true, 30},
		{bin, "greenbelt_bush", "中央绿化灌木丛", false, 12},
		{bin, "rain_well", "东门雨水井", true, 18},
		{lao, "construction_site", "旧改建筑工地基坑", false, 8},
	}
	for _, p := range points {
		if _, err := db.Exec(`INSERT INTO water_points(community_id, type, location_desc, source, larvae_found, created_at)
			VALUES($1,$2,$3,'manual',$4,$5)`, p.comm, p.typ, p.loc, p.larvae, time.Now().AddDate(0, 0, -p.daysAgo)); err != nil {
			log.Fatalf("seed water points: %v", err)
		}
	}

	// 儿童活动区（幼儿园/乐园 + 园方联系人 + 儿童活动时段 + 家长群）
	type cz struct {
		comm            int64
		name, typ       string
		contact, phone  string
		contactUser     string
		activity, group string
	}
	zones := []cz{
		{yang, "阳光幼儿园旁绿化带", "kindergarten", "王园", "13911110001", "kindergarten", "07:30-08:30,11:30-13:30,16:00-18:00", "阳光幼儿园家长一群"},
		{bin, "滨江儿童乐园", "playground", "李园长", "13911110002", "kindergarten2", "08:00-10:00,15:00-18:30", "滨江乐园家长群"},
	}
	for _, z := range zones {
		var contactUID *int64
		if z.contactUser != "" {
			if id, ok := userIDs[z.contactUser]; ok {
				contactUID = &id
			}
		}
		if _, err := db.Exec(`INSERT INTO child_zones(community_id, name, zone_type, contact_name, contact_phone, contact_user_id, activity_times, parent_group)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, z.comm, z.name, z.typ, z.contact, z.phone, contactUID, z.activity, z.group); err != nil {
			log.Fatalf("seed child zones: %v", err)
		}
	}

	// 物业积水整改演示（地下室排水沟长期积水 → 消杀队临时处理 → 物业排水整改 → 按图复查）
	type wpID struct {
		loc string
		id  int64
	}
	b2id := wpID{loc: "地下车库 B2 集水井"}
	if err := db.QueryRow(`SELECT id FROM water_points WHERE community_id=$1 AND location_desc=$2`, yang, b2id.loc).Scan(&b2id.id); err != nil {
		b2id.id = 0
	}
	var binBaseID int64
	if err := db.QueryRow(`INSERT INTO water_points(community_id, type, location_desc, source, larvae_found, status, created_at)
		VALUES($1,'basement_damp','地下车库 A 区排水沟','manual',true,'rectifying',$2) RETURNING id`,
		bin, time.Now().AddDate(0, 0, -5)).Scan(&binBaseID); err != nil {
		log.Fatalf("seed binjiang basement point: %v", err)
	}
	var yangBase2ID int64
	if err := db.QueryRow(`INSERT INTO water_points(community_id, type, location_desc, source, larvae_found, status, created_at)
		VALUES($1,'basement_damp','地下车库 B1 排水沟','manual',false,'cleared',$2) RETURNING id`,
		yang, time.Now().AddDate(0, 0, -20)).Scan(&yangBase2ID); err != nil {
		log.Fatalf("seed yang basement point: %v", err)
	}

	// 同点投诉归因演示：同小区 A/B/C 多处地下室积水，各点投诉必须分别归因，互不串算。
	// 插入后由启动回填 backfillPropRectComplaints 按 water_point_id/标准化位置统一计算基线与当前值。
	baseComplaint := func(reporter string, comm int64, loc string, daysAgo int) {
		var rid int64
		if err := db.QueryRow(`INSERT INTO reports(report_no, reporter_id, community_id, type, location_desc, nearby_population, has_pets, description, status, created_at)
			VALUES('',$1,$2,'basement_damp',$3,'车主',false,'地下室排水沟长期积水有异味蚊虫多','closed',$4) RETURNING id`,
			userIDs[reporter], comm, loc, time.Now().AddDate(0, 0, -daysAgo)).Scan(&rid); err != nil {
			log.Fatalf("seed basement complaint: %v", err)
		}
		db.Exec(`UPDATE reports SET report_no='RPT'||LPAD(id::text,8,'0') WHERE id=$1`, rid)
	}
	// 阳光小区 B2 集水井：建档(-6天) 前 2 起（-25/-9 天），建档后 1 起（-2 天，待复查时计入）
	baseComplaint("resident", yang, "地下车库 B2 集水井", 25)
	baseComplaint("grid", yang, "地下车库 B2 集水井", 9)
	baseComplaint("resident", yang, "地下车库 B2 集水井", 2)
	// 阳光小区 B1 排水沟：建档(-12) 前 2 起（-30/-13 天）；复查(-8) 后又新增 1 起（-3 天，应被冻结不计入）
	baseComplaint("resident", yang, "地下车库 B1 排水沟", 30)
	baseComplaint("property", yang, "地下车库 B1 排水沟", 13)
	baseComplaint("resident", yang, "地下车库 B1 排水沟", 3)
	// 阳光小区 C 区排水沟（干扰点，无整改任务）：同小区他点投诉，不得计入 B2/B1
	baseComplaint("grid", yang, "地下车库 C 区排水沟", 10)
	// 滨江花园 A 区排水沟：建档(-9) 前 1 起（-12 天）
	baseComplaint("resident2", bin, "地下车库 A 区排水沟", 12)
	// 滨江花园 B 区排水沟（干扰点）：建档后新增（-4 天），复查 A 任务时不得计入 A
	baseComplaint("property2", bin, "地下车库 B 区排水沟", 4)

	// ① 整改超期：阳光小区 B2 集水井，消杀队已临时处理 2 次，物业尚未完成（复查日期已过 3 天）
	var pr1 int64
	if err := db.QueryRow(`INSERT INTO property_rectifications
		(work_order_id, water_point_id, community_id, water_location, water_type,
		 temp_treated_by, temp_treatment, temp_treated_at, temp_treatment_times,
		 facility_user_id, recheck_date, complaints_before, status, created_by, created_at)
		VALUES(NULL,$1,$2,$3,'underground_garage',$4,$5,$6,2,$7,$8,0,'pending',$9,$10) RETURNING id`,
		b2id.id, yang, b2id.loc,
		userIDs["operator"], "消杀队临时抽排+投药灭孑孓，排水沟长期返水需物业工程维修", time.Now().AddDate(0, 0, -6),
		userIDs["property"], time.Now().AddDate(0, 0, -3), userIDs["operator"], time.Now().AddDate(0, 0, -6)).Scan(&pr1); err != nil {
		log.Fatalf("seed prop rect 1: %v", err)
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, pr1)

	// ② 复查超期：滨江花园 A 区排水沟，物业已报审（照片标注位置+方式），但复查日期已过 2 天仍未按图核验
	var pr2 int64
	if err := db.QueryRow(`INSERT INTO property_rectifications
		(water_point_id, community_id, water_location, water_type,
		 temp_treated_by, temp_treatment, temp_treated_at, temp_treatment_times,
		 facility_user_id, recheck_date, repair_desc, repair_method, rectify_photos, rectify_photo_remark,
		 rectified_by, rectified_at, complaints_before, status, created_by, created_at)
		VALUES($1,$2,$3,'basement_damp',$4,$5,$6,2,$7,$8,
		 $9,'dredge_drain',$10::jsonb,$11,$12,$13,0,'recheck_pending',$4,$14) RETURNING id`,
		binBaseID, bin, "地下车库 A 区排水沟",
		userIDs["operator2"], "临时抽排积水并投药", time.Now().AddDate(0, 0, -9),
		userIDs["property2"], time.Now().AddDate(0, 0, -2),
		"已清掏排水沟淤积泥沙，修复 A 区集水井排水泵，排水沟恢复通畅无积水",
		`["https://example.com/rect-bin-a-1.jpg","https://example.com/rect-bin-a-2.jpg"]`,
		"照片①位置：地下车库 A 区排水沟起点（红色标识积水点）；处理方式：清掏淤积、更换排水泵。照片②位置：A 区集水井；处理方式：维修后复拍，沟通无积水",
		userIDs["property2"], time.Now().AddDate(0, 0, -3),
		time.Now().AddDate(0, 0, -9)).Scan(&pr2); err != nil {
		log.Fatalf("seed prop rect 2: %v", err)
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, pr2)

	// ③ 复查通过 + 投诉下降：阳光小区 B1 排水沟已整改并按图核验通过，居民投诉由 2 起降为 0
	var pr3 int64
	if err := db.QueryRow(`INSERT INTO property_rectifications
		(water_point_id, community_id, water_location, water_type,
		 temp_treated_by, temp_treatment, temp_treated_at, temp_treatment_times,
		 facility_user_id, recheck_date, repair_desc, repair_method, rectify_photos, rectify_photo_remark,
		 rectified_by, rectified_at,
		 recheck_photos, recheck_remark, recheck_result, rechecked_by, rechecked_at,
		 complaints_before, complaints_after, status, created_by, created_at)
		VALUES($1,$2,$3,'basement_damp',$4,$5,$6,2,$7,$8,
		 $9,'rebuild_drain',$10::jsonb,$11,$7,$12,
		 $13::jsonb,$14,'pass',$15,$16,2,0,'verified',$4,$17) RETURNING id`,
		yangBase2ID, yang, "地下车库 B1 排水沟",
		userIDs["operator"], "临时投药处理 2 次", time.Now().AddDate(0, 0, -12),
		userIDs["property"], time.Now().AddDate(0, 0, -9),
		"重做排水沟找坡并增设溢流管，雨后 30 分钟内排干",
		`["https://example.com/rect-yang-b1-before.jpg","https://example.com/rect-yang-b1-after.jpg"]`,
		"照片①位置：B1 排水沟低洼点（黄色圈注积水位置），处理方式：重做找坡；照片②位置：同一位置整改后，处理方式：增设溢流管",
		time.Now().AddDate(0, 0, -10),
		`["https://example.com/recheck-yang-b1.jpg"]`,
		"对照整改照片同一角度复查，排水沟无积水、无孑孓，按图核验通过",
		userIDs["grid"], time.Now().AddDate(0, 0, -8),
		time.Now().AddDate(0, 0, -12)).Scan(&pr3); err != nil {
		log.Fatalf("seed prop rect 3: %v", err)
	}
	db.Exec(`UPDATE property_rectifications SET rect_no='PR'||LPAD(id::text,8,'0') WHERE id=$1`, pr3)

	// ========== 重点风险期应急响应与跨小区联防调度（演示） ==========
	seedEmergency(commIDs, teamIDs, userIDs)

	// ========== 居民拒绝入户与入户授权（演示：多方沟通中，版本化） ==========
	seedAccessCase(commIDs, teamIDs, userIDs)

	refreshKeyWaterPoints()
	log.Println("seed done")
}

// seedEmergency 构造一个进行中的多小区联防应急（登革热病例）与一个已解除归档的历史应急
func seedEmergency(commIDs, teamIDs map[string]int64, userIDs map[string]int64) {
	yang, bin, lao := commIDs["阳光小区"], commIDs["滨江花园"], commIDs["老城厢社区"]
	t2 := teamIDs["消杀二队"]

	// 进行中应急：周边出现确诊病例 → 阳光主疫区 + 滨江周边联防
	var eid int64
	if err := db.QueryRow(`INSERT INTO emergency_responses(title, trigger_type, disease, description, recheck_interval_days, status, created_by, started_at)
		VALUES('登革热确诊病例联防应急','nearby_case','登革热','阳光小区发现 1 例登革热确诊，病例活动轨迹涉及 7 号楼楼顶与北门堆放点；同步提升滨江花园为周边联防。应急期间复查频次提高到每日一次。',1,'active',$1,$2) RETURNING id`,
		userIDs["street"], time.Now().AddDate(0, 0, -2)).Scan(&eid); err != nil {
		log.Fatalf("seed emergency: %v", err)
	}
	db.Exec(`UPDATE emergency_responses SET emerg_no='EM'||LPAD(id::text,8,'0') WHERE id=$1`, eid)

	type ec struct {
		cid    int64
		role   string
		risk   string
		reason string
	}
	ecm := []ec{
		{yang, "affected", "emergency", "确诊病例活动轨迹涉及本小区多处积水点"},
		{bin, "surrounding", "warning", "与主疫区相邻，纳入跨小区联防"},
	}
	ecOrder := map[int64]int64{}
	for _, x := range ecm {
		var cstart int
		db.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1`, x.cid).Scan(&cstart)
		var oid int64
		if err := db.QueryRow(`INSERT INTO work_orders(community_id, priority, status, created_by, emergency_response_id)
			VALUES($1,100,'escalated',$2,$3) RETURNING id`, x.cid, userIDs["street"], eid).Scan(&oid); err != nil {
			log.Fatalf("seed emergency order: %v", err)
		}
		db.Exec(`UPDATE work_orders SET order_no='WO'||LPAD(id::text,8,'0') WHERE id=$1`, oid)
		db.Exec(`INSERT INTO emergency_communities(emergency_id, community_id, role, risk_level, risk_reason, work_order_id, complaints_at_start)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, eid, x.cid, x.role, x.risk, x.reason, oid, cstart)
		ecOrder[x.cid] = oid
		db.Exec(`UPDATE communities SET risk_level=$1, risk_reason=$2, risk_updated_at=now() WHERE id=$3`, x.risk, x.reason, x.cid)
		ensureAllParties(oid)
		addLog(oid, nil, "启动应急响应", "登革热确诊病例联防应急启动，复查频次提高为每 1 天一次，五方同单协同")
	}

	// 病例与活动轨迹（场所与积水点位置一致 → 快照标记“邻近病例活动轨迹”）
	cases := []emergCaseReq{
		{CommunityID: yang, CaseStatus: "confirmed", PatientAlias: "张某（脱敏）", OnsetDate: time.Now().AddDate(0, 0, -3).Format("2006-01-02"), Source: "疾控通报",
			Trajectory: []map[string]any{
				{"time": time.Now().AddDate(0, 0, -5).Format("2006-01-02 15:04") + " 09:00", "place": "7 号楼楼顶水箱", "note": "上楼顶晾晒"},
				{"time": time.Now().AddDate(0, 0, -5).Format("2006-01-02") + " 18:00", "place": "北门废旧轮胎堆放点", "note": "取物停留约 20 分钟"},
			}},
	}
	traj1, _ := json.Marshal(cases[0].Trajectory)
	db.Exec(`INSERT INTO emergency_cases(emergency_id, community_id, case_status, patient_alias, onset_date, trajectory, source)
		VALUES($1,$2,'confirmed',$3,$4,$5,'疾控通报')`, eid, yang, "张某（脱敏）", cases[0].OnsetDate, RawJSON(traj1))

	// 重点积水点快照（楼顶水箱/地下车库/绿化带/雨水井/建筑工地等，关联轨迹）
	snapshotEmergencyWaterPoints(eid, []int64{yang, bin}, cases)

	// 跨小区支援：消杀二队（负责滨江）跨小区支援阳光主疫区
	db.Exec(`INSERT INTO emergency_dispatch(emergency_id, from_community_id, to_community_id, team_id, resource_type, resource_ref, action, created_by)
		VALUES($1,$2,$3,$4,'team','跨小区应急消杀队','二队抽组 3 人支援阳光楼顶水箱与地下车库加密处置',$5)`,
		eid, bin, yang, t2, userIDs["street"])
	addTeamParties(ecOrder[yang], t2)
	addLog(ecOrder[yang], nil, "跨小区支援", "调度「消杀二队」滨江花园 → 阳光小区 支援应急消杀")
	// 网格 + 卫监力量
	db.Exec(`INSERT INTO emergency_dispatch(emergency_id, to_community_id, resource_type, resource_ref, action, created_by)
		VALUES($1,$2,'grid','社区网格员 4 名','入户调查与布雷图指数监测',$3)`, eid, yang, userIDs["street"])

	// 昨日每日汇总（投诉变化/积水点清零/药剂消耗/整改责任）
	yday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	db.Exec(`INSERT INTO emergency_daily_summaries(emergency_id, community_id, summary_date, new_complaints, open_water_points, cleared_water_points, chemical_used, treatments, rect_open, rect_overdue, note)
		VALUES($1,NULL,$2,3,6,2,12.5,4,1,1,'应急第 1 天合计：新增投诉 3，清除积水点 2，药剂 12.5')`,
		eid, yday)
	db.Exec(`INSERT INTO emergency_daily_summaries(emergency_id, community_id, summary_date, new_complaints, open_water_points, cleared_water_points, chemical_used, treatments, rect_open, rect_overdue)
		VALUES($1,$2,$3,3,4,2,9.0,3,1,1),($1,$4,$3,0,2,0,3.5,1,0,0)`,
		eid, yang, yday, bin)

	// 历史应急（老城厢，已解除并归档）：风险已自动降级为常态
	var eid2 int64
	if err := db.QueryRow(`INSERT INTO emergency_responses(title, trigger_type, disease, description, recheck_interval_days, status, created_by, started_at, resolved_at, resolve_note)
		VALUES('老旧工地积水应急','cdc_warning','登革热','疾控蚊媒密度预警，旧改工地基坑积水处置。',2,'resolved',$1,$2,$3,'积水点全部清除，连续两周无新发病例，解除并归档') RETURNING id`,
		userIDs["street"], time.Now().AddDate(0, 0, -40), time.Now().AddDate(0, 0, -33)).Scan(&eid2); err != nil {
		log.Fatalf("seed emergency 2: %v", err)
	}
	db.Exec(`UPDATE emergency_responses SET emerg_no='EM'||LPAD(id::text,8,'0') WHERE id=$1`, eid2)
	var oid2 int64
	db.QueryRow(`INSERT INTO work_orders(community_id, priority, status, created_by, emergency_response_id, closed_at)
		VALUES($1,90,'closed',$2,$3,$4) RETURNING id`, lao, userIDs["street"], eid2, time.Now().AddDate(0, 0, -33)).Scan(&oid2)
	db.Exec(`UPDATE work_orders SET order_no='WO'||LPAD(id::text,8,'0') WHERE id=$1`, oid2)
	db.Exec(`INSERT INTO emergency_communities(emergency_id, community_id, role, risk_level, risk_reason, work_order_id, complaints_at_start)
		VALUES($1,$2,'affected','emergency','疾控预警：旧改工地基坑积水',$3,2)`, eid2, lao, oid2)
	snapshotEmergencyWaterPoints(eid2, []int64{lao}, nil)
	db.Exec(`INSERT INTO emergency_daily_summaries(emergency_id, community_id, summary_date, new_complaints, open_water_points, cleared_water_points, chemical_used, treatments, rect_open, rect_overdue)
		VALUES($1,NULL,$2,1,1,1,6.0,2,0,0)`, eid2, time.Now().AddDate(0, 0, -34).Format("2006-01-02"))
	// 历史应急小区风险已降级为常态
	db.Exec(`UPDATE communities SET risk_level='normal', risk_reason='', risk_updated_at=now() WHERE id=$1`, lao)
}

// seedAccessCase 构造一个“居民拒绝入户、多方沟通中”的入户授权案例（版本化）
func seedAccessCase(commIDs, teamIDs map[string]int64, userIDs map[string]int64) {
	yang := commIDs["阳光小区"]
	// 关联住户投诉（户内蚊虫）与一张不强行派单的协同工单
	var rid int64
	if err := db.QueryRow(`INSERT INTO reports(report_no, reporter_id, community_id, type, location_desc, nearby_population, has_pets, description, status, created_at)
		VALUES('', $1, $2, 'mosquito_dense', '3号楼2单元501室', '有老人与儿童', true, '家中蚊虫多，担心药剂影响孩子和宠物，暂不愿入户', 'processing', $3) RETURNING id`,
		userIDs["resident"], yang, time.Now().AddDate(0, 0, -2)).Scan(&rid); err != nil {
		log.Fatalf("seed access report: %v", err)
	}
	db.Exec(`UPDATE reports SET report_no='RPT'||LPAD(id::text,8,'0') WHERE id=$1`, rid)

	var oid int64
	if err := db.QueryRow(`INSERT INTO work_orders(community_id, report_id, priority, status, created_by, created_at)
		VALUES($1,$2,60,'escalated',$3,$4) RETURNING id`, yang, rid, userIDs["street"], time.Now().AddDate(0, 0, -2)).Scan(&oid); err != nil {
		log.Fatalf("seed access order: %v", err)
	}
	db.Exec(`UPDATE work_orders SET order_no='WO'||LPAD(id::text,8,'0') WHERE id=$1`, oid)
	ensureAllParties(oid)

	var aid int64
	if err := db.QueryRow(`INSERT INTO access_cases
		(work_order_id, community_id, resident_user_id, address, status, reject_reasons, reject_note, sensitive_groups,
		 pets_desc, acceptable_times, acceptable_chemicals, outdoor_allowed, outdoor_areas, recorded_by, rejected_at,
		 witnesses, final_opinion, created_at, updated_at)
		VALUES($1,$2,$3,'3号楼2单元501室','negotiating',
		 $4::jsonb,'担心药剂气味与残留，希望先看到告知书和药剂说明',$5::jsonb,
		 '家有一只小型犬','工作日 19:00 后或周末上午','低毒、有安全间隔说明的药剂，倾向苏云金杆菌(BTI)',
		 true,$6::jsonb,$7,$8,
		 '楼栋长王阿姨、物业管家','居民愿意再沟通，要求陪同人在场并提供书面告知',$9,$9) RETURNING id`,
		oid, yang, userIDs["resident"],
		`["pets","children","distrust_chemical"]`,
		`["children","elderly"]`,
		`["doorway","staircase","balcony","sewer"]`,
		userIDs["grid"], time.Now().AddDate(0, 0, -2),
		time.Now().AddDate(0, 0, -2)).Scan(&aid); err != nil {
		log.Fatalf("seed access case: %v", err)
	}
	db.Exec(`UPDATE access_cases SET case_no='AC'||LPAD(id::text,8,'0') WHERE id=$1`, aid)

	addLog(oid, nil, "居民拒绝入户", "网格员登记：不强行派单，五方同单协商；居民可接受工作日晚间/周末上午，允许门口/楼道/阳台外围/下水道处理")
	addLog(oid, nil, "入户沟通", "街道、物业、楼栋长首次上门，居民要求书面告知书与陪同人，待二次沟通（见证人：楼栋长王阿姨、物业管家）")

	// 版本化：v1 登记拒绝、v2 上门沟通
	db.Exec(`INSERT INTO access_case_versions(case_id, version_no, action, actor_id, actor_role, snapshot, note)
		SELECT $1,1,'登记拒绝入户',$2,'grid', to_jsonb(t),'居民因宠物/儿童/药剂顾虑拒绝入户'
		FROM (SELECT * FROM access_cases WHERE id=$1) t`, aid, userIDs["grid"])
	db.Exec(`INSERT INTO access_case_versions(case_id, version_no, action, actor_id, actor_role, snapshot, note)
		SELECT $1,2,'上门沟通记录',$2,'street', to_jsonb(t),'首次上门，记录见证人与居民最终意见'
		FROM (SELECT * FROM access_cases WHERE id=$1) t`, aid, userIDs["street"])
}
