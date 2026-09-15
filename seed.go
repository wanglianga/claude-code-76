package main

import (
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

	refreshKeyWaterPoints()
	log.Println("seed done")
}
