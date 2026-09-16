#!/usr/bin/env python3
"""关键业务流验证：上报→派单→消杀→复查→异常协同→整改→告知→闭环→看板"""
import json, urllib.request, sys

import os
from datetime import datetime, timedelta
B = os.environ.get("BASE_URL", "http://localhost:3076")
PASS, FAIL = 0, 0

def call(method, path, token=None, body=None, expect_ok=True):
    req = urllib.request.Request(B + path, method=method)
    req.add_header("Content-Type", "application/json")
    if token: req.add_header("Authorization", "Bearer " + token)
    data = None
    if body is not None: data = json.dumps(body).encode()
    try:
        with urllib.request.urlopen(req, data, timeout=15) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")

def check(name, cond, extra=""):
    global PASS, FAIL
    if cond: PASS += 1; print(f"  ✔ {name} {extra}")
    else: FAIL += 1; print(f"  ✘ {name} {extra}")

def login(u, p):
    s, r = call("POST", "/api/auth/login", body={"username": u, "password": p})
    assert s == 200, f"login {u} failed: {r}"
    return r["data"]["token"]

print("== 1. 六角色登录 ==")
toks = {
    "resident": login("resident", "Resident@123"),
    "property": login("property", "Property@123"),
    "grid": login("grid", "Grid@123"),
    "operator": login("operator", "Operator@123"),
    "street": login("street", "Street@123"),
    "supervisor": login("supervisor", "Supervisor@123"),
}
check("六角色全部登录成功", all(toks.values()))

print("== 2. 居民上报（位置/照片/周边人群/宠物） ==")
s, r = call("POST", "/api/reports", toks["resident"], {
    "type": "child_bite", "location_desc": "中心花园儿童滑梯旁", "nearby_population": "老人儿童居多",
    "has_pets": True, "photos": ["https://example.com/bite1.jpg"], "description": "孩子被叮咬多处红肿"})
check("儿童被叮咬上报", s == 200 and r["data"]["report_no"].startswith("RPT"))
s, r = call("POST", "/api/reports", toks["resident"], {
    "type": "rooftop_water", "location_desc": "5号楼楼顶", "nearby_population": "顶楼住户",
    "has_pets": False, "description": "雨后积水两天"})
roof_report = r["data"]
check("楼顶积水上报", s == 200)
s, r = call("GET", "/api/water-points?type=rooftop_water", toks["street"])
auto_wp = [w for w in r["data"] if w["source"] == "report"]
check("积水类上报自动生成积水点", len(auto_wp) >= 1)

print("== 3. 街道派单（评分含降雨/投诉密度/风险期） ==")
s, r = call("GET", "/api/dispatch/preview", toks["street"])
cands = r["data"]["candidates"]
check("派单预览有候选", len(cands) > 0, f"({len(cands)} 条)")
check("评分含风险期加权", any("风险期" in "".join(c["reasons"]) for c in cands))
check("评分含降雨因子", any("降雨" in "".join(c["reasons"]) for c in cands))
s, r = call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
gen = r["data"]
check("生成派单", s == 200 and gen["created"] > 0, f"(创建 {gen['created']}，指派 {gen['assigned']})")

print("== 4. 消杀人员开工并提交消杀记录 ==")
s, r = call("GET", "/api/work-orders?status=assigned", toks["operator"])
orders = r["data"]
check("消杀一队有已派工单", len(orders) > 0, f"({len(orders)} 单)")
oid = orders[0]["id"]
s, r = call("POST", f"/api/work-orders/{oid}/start", toks["operator"], {})
check("到场开工", s == 200)
s, r = call("GET", "/api/chemicals", toks["operator"])
chem = r["data"][0]
stock_before = chem["stock"]
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "中心花园及周边绿化带",
    "chemical_used": 2.5, "warning_sign": True, "resident_notified": True, "pet_avoided": True,
    "new_water_point_found": True, "new_water_point_desc": "5号楼后废弃花盆积水"})
check("提交消杀记录", s == 200, f"({r['data']['message']})")
s, r = call("GET", "/api/chemicals", toks["operator"])
check("药剂库存已扣减", abs(r["data"][0]["stock"] - (stock_before - 2.5)) < 1e-9,
      f"({stock_before} -> {r['data'][0]['stock']})")
s, r = call("GET", "/api/work-orders/" + str(oid), toks["operator"])
d = r["data"]
check("工单转待复查且有复查期限", d["order"]["status"] == "recheck_pending" and d["order"]["recheck_due_at"])
check("消杀记录含警示牌/告知/宠物避让", d["treatments"][0]["warning_sign"] and d["treatments"][0]["resident_notified"] and d["treatments"][0]["pet_avoided"])
s, r = call("GET", "/api/water-points?type=other", toks["street"])
check("新积水点已登记", any("废弃花盆" in w["location_desc"] for w in r["data"]))

print("== 5. 复查发现幼虫 → 五方协同 ==")
s, r = call("POST", f"/api/work-orders/{oid}/rechecks", toks["supervisor"], {"result": "larvae_found", "notes": "集水井仍见孑孓"})
check("复查提交（有幼虫）", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
d = r["data"]
roles = {p["role"] for p in d["parties"]}
check("工单升级为异常协同", d["order"]["status"] == "escalated")
check("五方（居民/物业/消杀/街道/卫监）同单协同", {"resident","property","operator","street","supervisor"} <= roles, f"({roles})")
check("自动生成「复查仍有幼虫」异常", any(i["type"] == "larvae_remaining" and i["status"] == "open" for i in d["issues"]))

print("== 6. 物业设施异常 + 整改流转 ==")
s, r = call("POST", f"/api/work-orders/{oid}/issues", toks["property"], {"type": "property_facility", "description": "积水由楼顶水箱排水管破裂造成"})
check("物业设施异常上报", s == 200)
s, r = call("POST", f"/api/work-orders/{oid}/rectifications", toks["street"], {"description": "修复排水管并清理积水", "deadline": "2026-09-20"})
check("街道下发整改", s == 200)
rect_id = r["data"]["id"]
s, r = call("GET", "/api/rectifications", toks["property"])
check("物业可见整改任务", any(x["id"] == rect_id for x in r["data"]))
s, r = call("POST", f"/api/rectifications/{rect_id}/start", toks["property"], {})
check("物业受理", s == 200)
s, r = call("POST", f"/api/rectifications/{rect_id}/complete", toks["property"], {})
check("物业完成整改", s == 200)
s, r = call("POST", f"/api/rectifications/{rect_id}/verify", toks["supervisor"], {"pass": True, "notes": "现场核验通过"})
check("卫生监督核验通过", s == 200)

print("== 7. 异常解决 + 居民告知 ==")
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
for i in r["data"]["issues"]:
    if i["status"] == "open":
        s2, r2 = call("POST", f"/api/work-orders/{oid}/issues/{i['id']}/resolve", toks["street"], {"resolution": "已协调处理完毕"})
        assert s2 == 200, r2
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
check("异常全部解决", all(i["status"] == "resolved" for i in r["data"]["issues"]))
s, r = call("POST", "/api/notifications", toks["street"], {"work_order_id": oid, "title": "消杀作业通知", "content": "本周五上午对中心花园消杀，请看管宠物", "channel": "app"})
check("发布居民告知", s == 200)
s, r = call("GET", "/api/notifications", toks["resident"])
check("居民可见告知", any("消杀作业通知" in (n["title"] or "") for n in r["data"]))

print("== 8. 二次消杀 → 复查通过 → 自动闭环 ==")
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "5号楼楼顶及集水井",
    "chemical_used": 1.5, "warning_sign": True, "resident_notified": True, "pet_avoided": True})
check("二次消杀", s == 200)
s, r = call("POST", f"/api/work-orders/{oid}/rechecks", toks["grid"], {"result": "pass", "notes": "未见幼虫"})
check("复查通过", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
d = r["data"]
check("工单自动闭环", d["order"]["status"] == "closed", f"(状态 {d['order']['status_label']})")
check("闭环条件全满足", all(d["close_conditions"].values()))
s, r = call("GET", "/api/reports", toks["resident"])
check("关联上报已闭环", all(x["status"] == "closed" for x in r["data"] if x["id"] == d["report"]["id"]) if d.get("report") else True)

print("== 9. 闭环看板 / 考核 / 趋势 / 重点清单 / 风险期 ==")
s, r = call("GET", "/api/dashboard/closed-loop", toks["street"])
rows = r["data"]["rows"]
yang = next((x for x in rows if x["community_name"] == "阳光小区"), None)
check("闭环看板有小区数据", yang is not None and yang["orders_closed"] >= 1, f"(阳光小区闭环 {yang['orders_closed'] if yang else 0} 单)")
check("风险期生效且复查间隔缩短", r["data"]["risk_period"] and r["data"]["recheck_interval_days"] == 3)
s, r = call("GET", "/api/dashboard/assessment?month=2026-09", toks["supervisor"])
check("考核：小区维度药剂消耗>0", any(x["chemical_used"] > 0 for x in r["data"]["communities"]))
check("考核：消杀队维度有数据", any(t["treatments"] > 0 for t in r["data"]["teams"]))
s, r = call("GET", "/api/dashboard/complaint-trend", toks["street"])
check("投诉趋势有数据", len(r["data"]) > 0)
s, r = call("GET", "/api/water-points/key-list", toks["street"])
check("重点积水点清单自动生成", len(r["data"]["list"]) > 0, f"({len(r['data']['list'])} 处)")
s, r = call("GET", "/api/risk-periods", toks["street"])
check("风险期列表含登革热", any("登革热" in x["disease"] for x in r["data"]["list"]))

print("== 10. 权限与库存边界 ==")
s, r = call("POST", "/api/dispatch/generate", toks["resident"], {})
check("居民无权派单(403)", s == 403)
s, r = call("GET", "/api/work-orders", None)
check("未登录被拒绝(401)", s == 401)
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "x", "chemical_used": 99999})
check("已闭环工单不可再消杀(409)", s == 409)

print("== 11. 儿童活动区错峰消杀 ==")
toks["kindergarten"] = login("kindergarten", "Kindergarten@123")
check("园方联系人登录", bool(toks["kindergarten"]))
s, r = call("GET", "/api/child-zones", toks["street"])
zones = r["data"]
zone = next((z for z in zones if "幼儿园" in z["name"]), None)
check("儿童活动区已登记（含联系人/活动时段/家长群）", zone and zone["contact_name"] and zone["activity_times"] and zone["parent_group"])
tomorrow = datetime.now() + timedelta(days=1)
def dt(h, m=0): return (tomorrow.replace(hour=h, minute=m)).strftime("%Y-%m-%d %H:%M")
# 与儿童活动时间冲突 → 应被拒绝（活动时段 16:00-18:00）
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "planned_start": dt(17), "planned_end": dt(18),
    "wind_direction": "东南风", "safety_interval_hours": 1})
check("与儿童活动时间冲突的计划被拒绝(409)", s == 409, f"({r.get('message','')[:40]}...)")
# 错峰时段（晚间）→ 通过
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "planned_start": dt(20), "planned_end": dt(21),
    "wind_direction": "东南风", "safety_interval_hours": 2})
plan = r.get("data") or {}
check("错峰计划创建成功", s == 200 and plan.get("status") == "notified")
check("恢复时间=结束+安全间隔", plan.get("recovery_time", "").startswith((tomorrow).strftime("%Y-%m-%d")) and "23:00" in plan.get("recovery_time", ""))
pid = plan.get("id")
s, r = call("GET", f"/api/child-zone-plans/{pid}", toks["street"])
rems = r["data"]["reminders"]
check("提醒推送幼儿园/家长群/附近居民", {x["audience"] for x in rems} == {"kindergarten", "parents", "residents"})
check("提醒标明避让时段与联系人", all(x["avoid_period"] and x["contact_info"] for x in rems))
check("提醒保留送达状态(已发送)", all(x["delivery_status"] == "sent" for x in rems))
kg_rem = next(x for x in rems if x["audience"] == "kindergarten")
s, r = call("POST", f"/api/child-zone-plans/{pid}/reminders/{kg_rem['id']}/deliver", toks["kindergarten"], {})
check("幼儿园提醒确认送达", s == 200)
# 关联工单：新上报→派单→消杀→计划自动已作业→撤警示→园方确认→居民可见
call("POST", "/api/reports", toks["resident"], {"type": "greenbelt_water", "location_desc": "阳光幼儿园旁绿化带", "nearby_population": "儿童", "has_pets": False})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
orders = call("GET", "/api/work-orders?status=assigned", toks["operator"])[1]["data"]
oid2 = orders[0]["id"]
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "work_order_id": oid2, "planned_start": dt(20, 30), "planned_end": dt(21, 30),
    "wind_direction": "北风", "safety_interval_hours": 2})
pid2 = r["data"]["id"]
check("关联工单的错峰计划", s == 200)
call("POST", f"/api/work-orders/{oid2}/start", toks["operator"], {})
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/work-orders/{oid2}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "幼儿园旁绿化带",
    "chemical_used": 1.0, "warning_sign": True, "resident_notified": True, "pet_avoided": True})
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
check("消杀提交后计划自动转「已作业」", r["data"]["plan"]["status"] == "treated")
s, r = call("POST", f"/api/child-zone-plans/{pid2}/confirm", toks["kindergarten"], {"note": "提前确认"})
check("未撤警示不可园方确认(409)", s == 409)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/remove-warning", toks["operator"], {})
check("警示撤除", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid2), toks["street"])
check("警示撤除进入工单时间线", any("警示撤除" in l["action"] for l in r["data"]["logs"]))
s, r = call("POST", f"/api/child-zone-plans/{pid2}/confirm", toks["kindergarten"], {"note": "现场已恢复安全"})
check("园方确认", s == 200 and call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])[1]["data"]["plan"]["status"] == "confirmed")
s, r = call("GET", "/api/work-orders/" + str(oid2), toks["street"])
check("园方确认进入工单时间线", any("园方确认" in l["action"] for l in r["data"]["logs"]))
s, r = call("GET", "/api/notifications", toks["resident"])
rec_notice = next((n for n in r["data"] if "儿童活动恢复" in (n["title"] or "")), None)
check("居民端可见安全间隔与恢复时间", rec_notice and "安全间隔" in rec_notice["content"] and "恢复时间" in rec_notice["content"])

print("== 12. 计划详情与提醒送达范围门禁 ==")
toks["resident2"] = login("resident2", "Resident@123")
toks["operator2"] = login("operator2", "Operator@123")
# pid2 = 第11节已确认的滨江? 否——pid2 为阳光幼儿园关联工单计划（已 confirmed）；pid 为未确认计划
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["resident2"])
check("resident2 访问阳光小区计划详情被拒绝(403/404)", s in (403, 404))
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
rem0 = next(x for x in r["data"]["reminders"] if x["delivery_status"] == "sent")
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rem0['id']}/deliver", toks["resident2"], {})
check("resident2 更新提醒送达被拒绝(403/404)", s in (403, 404))
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
still = next(x for x in r["data"]["reminders"] if x["id"] == rem0["id"])
check("越权失败后送达状态数据不变", still["delivery_status"] == "sent")
# resident 本小区：未确认计划不可见，已确认计划仅公开字段
s, r = call("GET", f"/api/child-zone-plans/{pid}", toks["resident"])
check("居民不可见未确认计划详情(404)", s == 404)
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["resident"])
pub = r["data"]["plan"] if s == 200 else {}
check("居民可见已确认计划公开字段(安全间隔/恢复时间/避让)", s == 200 and pub.get("safety_interval_hours") and pub.get("recovery_time") and pub.get("avoid_period"))
check("居民详情不泄露园方电话与提醒明细", "contact_phone" not in pub and not r["data"].get("reminders"))
s, r = call("GET", "/api/child-zone-plans", toks["resident"])
check("居民列表仅已确认计划", all(p["status"] == "confirmed" for p in r["data"]))
check("居民列表不泄露园方电话", all(not p.get("contact_phone") for p in r["data"]))
# 园方联系人越权：滨江儿童乐园计划（未绑定王园）
zone2 = next(z for z in call("GET", "/api/child-zones", toks["street"])[1]["data"] if "滨江" in z["name"])
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone2["id"], "planned_start": dt(21), "planned_end": dt(22),
    "wind_direction": "西风", "safety_interval_hours": 2})
pid3 = r["data"]["id"]
s, r = call("GET", f"/api/child-zone-plans/{pid3}", toks["kindergarten"])
check("园方仅可读绑定活动区计划(404)", s == 404)
s, r = call("GET", f"/api/child-zone-plans/{pid3}", toks["resident2"])
check("本小区居民读未确认计划同样不可见(404)", s == 404)
# 送达权限：非作业队 operator2 → 403；关联 operator、street、绑定 kindergarten → 成功
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
rems2 = {x["audience"]: x for x in r["data"]["reminders"] if x["delivery_status"] == "sent"}
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['parents']['id']}/deliver", toks["operator2"], {})
check("非实际作业消杀队更新送达被拒绝(403)", s == 403)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['parents']['id']}/deliver", toks["operator"], {})
check("实际作业消杀队(operator)可更新送达", s == 200)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['residents']['id']}/deliver", toks["street"], {})
check("街道可更新送达", s == 200)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['kindergarten']['id']}/deliver", toks["kindergarten"], {})
check("绑定园方可更新送达", s == 200)
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
check("三类提醒全部送达", all(x["delivery_status"] == "delivered" for x in r["data"]["reminders"]))

print("== 13. 园方确认绑定门禁（未绑定/非绑定均 4xx 且无副作用） ==")
toks["kindergarten2"] = login("kindergarten2", "Kindergarten@123")
check("第二园方账号登录", bool(toks["kindergarten2"]))
# 街道创建未绑定联系人的活动区
s, r = call("POST", "/api/child-zones", toks["street"], {
    "community_id": 1, "name": "未绑定联系人活动区", "zone_type": "playground",
    "contact_name": "临时联系人", "contact_phone": "13000000000",
    "activity_times": "08:00-10:00", "parent_group": "临时家长群"})
check("创建未绑定联系人活动区", s == 200)
unbound_zone = r["data"]["id"]
# 新工单载体：上报→派单→取一单
call("POST", "/api/reports", toks["resident"], {"type": "mosquito_dense", "location_desc": "13号楼周边", "nearby_population": "居民", "has_pets": False})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
oid3 = call("GET", "/api/work-orders?status=assigned", toks["operator"])[1]["data"][0]["id"]
# 计划A=未绑定活动区；计划B=王园绑定活动区；挂同一工单
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": unbound_zone, "work_order_id": oid3, "planned_start": dt(20), "planned_end": dt(21),
    "wind_direction": "东风", "safety_interval_hours": 2})
planA = r["data"]["id"]
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "work_order_id": oid3, "planned_start": dt(21, 30), "planned_end": dt(22, 30),
    "wind_direction": "南风", "safety_interval_hours": 2})
planB = r["data"]["id"]
call("POST", f"/api/work-orders/{oid3}/start", toks["operator"], {})
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/work-orders/{oid3}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "13号楼及周边", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
for p in (planA, planB):
    call("POST", f"/api/child-zone-plans/{p}/remove-warning", toks["operator"], {})
stA = call("GET", f"/api/child-zone-plans/{planA}", toks["street"])[1]["data"]["plan"]["status"]
stB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]["status"]
check("两计划均推进到警示已撤除", stA == "warning_removed" and stB == "warning_removed")
# 未绑定：任意 kindergarten 确认 → 4xx，且无副作用
s, r = call("POST", f"/api/child-zone-plans/{planA}/confirm", toks["kindergarten"], {"note": "试图确认"})
check("未绑定活动区确认返回4xx", 400 <= s < 500, f"(HTTP {s})")
pA = call("GET", f"/api/child-zone-plans/{planA}", toks["street"])[1]["data"]["plan"]
check("计划状态保持 warning_removed 且确认字段为空", pA["status"] == "warning_removed" and not pA["confirmed_at"] and not pA["confirmed_by"])
s, r = call("GET", "/api/notifications", toks["street"])
check("无公开恢复通知", not any("未绑定联系人活动区" in (n["title"] or "") + (n["content"] or "") for n in r["data"]))
logs3 = call("GET", "/api/work-orders/" + str(oid3), toks["street"])[1]["data"]["logs"]
check("工单无园方确认日志", not any(l["action"] == "园方确认" for l in logs3))
# 非绑定账号：李园长确认王园的计划 → 4xx
s, r = call("POST", f"/api/child-zone-plans/{planB}/confirm", toks["kindergarten2"], {"note": "越权确认"})
check("非绑定园方账号确认返回4xx", 400 <= s < 500, f"(HTTP {s})")
pB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]
check("计划B状态未被改变", pB["status"] == "warning_removed" and not pB["confirmed_at"])
# 绑定园方确认自身计划 → 成功并发布恢复通知
s, r = call("POST", f"/api/child-zone-plans/{planB}/confirm", toks["kindergarten"], {"note": "确认恢复"})
check("绑定园方确认成功", s == 200)
pB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]
check("计划B已确认且确认字段已写入", pB["status"] == "confirmed" and pB["confirmed_at"] and pB["confirmed_by"])
s, r = call("GET", "/api/notifications", toks["resident"])
check("居民收到该计划恢复通知", any("阳光幼儿园" in (n["title"] or "") and "恢复时间" in (n["content"] or "") for n in r["data"]))

print("== 14. 宠物误触投诉处理 ==")
# 居民提交投诉：自动关联本小区最近一次消杀（药剂/区域/警示时间）
s, r = call("POST", "/api/pet-complaints", toks["resident"], {
    "pet_type": "dog", "pet_name": "豆豆", "symptom": "喷药后呕吐、精神萎靡",
    "walking_route": "18:00 从 3 号楼沿中心花园遛狗至东门",
    "medical_vouchers": ["https://example.com/vet-receipt-1.jpg"]})
pc = r.get("data") or {}
check("宠物投诉提交成功", s == 200 and pc.get("complaint_no", "").startswith("PC"))
check("自动关联药剂/喷洒区域/警示时间", bool(pc.get("chemical_name")) and bool(pc.get("spray_area")) and bool(pc.get("warning_time")))
check("行走路线已记录", pc.get("walking_route", "").startswith("18:00"))
pcid = pc.get("id")
pc_order = pc.get("work_order_id")
# 补充就医凭证
s, r = call("POST", f"/api/pet-complaints/{pcid}/vouchers", toks["resident"], {"vouchers": ["https://example.com/vet-receipt-2.jpg"]})
check("补充就医凭证", s == 200)
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["resident"])
check("凭证已合并", len(r["data"]["medical_vouchers"]) == 2)
# 他人不可见
s, r = call("GET", "/api/pet-complaints", toks["resident2"])
check("他人投诉不可见", all(p["id"] != pcid for p in r["data"]))
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["resident2"])
check("他人详情被拒绝(404)", s == 404)
# 居民无权办结
s, r = call("POST", f"/api/pet-complaints/{pcid}/handle", toks["resident"], {"resolution_note": "x"})
check("居民无权办结(403)", s == 403)
# 工单联动：异常与时间线
if pc_order:
    d = call("GET", "/api/work-orders/" + str(pc_order), toks["street"])[1]["data"]
    check("工单时间线含宠物投诉", any("宠物" in l["action"] for l in d["logs"]))
    check("未闭环工单生成宠物投诉异常", any(i["type"] == "pet_complaint" for i in d["issues"]))
# 卫生监督办结：回访+赔付+药剂说明+告知调整
s, r = call("POST", f"/api/pet-complaints/{pcid}/handle", toks["supervisor"], {
    "need_revisit": True, "compensation": True, "compensation_note": "凭就医票据报销",
    "chemical_note": "高效氯氟氰菊酯 1:100 稀释，安全间隔 4 小时，对犬类低毒",
    "notify_adjustment": "改为短信+公告栏双通道，提前 48 小时告知并标注宠物避让路线",
    "resolution_note": "已电话回访，宠物已好转"})
check("卫生监督办结", s == 200)
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["supervisor"])
d = r["data"]
check("办结记录完整（回访/赔付/药剂说明/告知调整）", d["status"] == "resolved" and d["need_revisit"] and d["compensation"] and d["chemical_note"] and d["notify_adjustment"])
# 告知调整后再发一起同类投诉 → 跟踪统计
s, r = call("POST", "/api/pet-complaints", toks["resident"], {
    "pet_type": "cat", "pet_name": "咪咪", "symptom": "打喷嚏", "walking_route": "中心花园散步"})
check("第二起投诉提交", s == 200)
s, r = call("GET", "/api/pet-complaints/tracking?community_id=1", toks["street"])
t = r["data"]
check("跟踪含最新告知调整", t["latest_adjustment"].startswith("改为短信"))
check("调整后同类投诉被统计", t["after_count"] >= 1 and t["decreased"] is not None)
# 小区消杀档案
s, r = call("GET", "/api/communities/1/archive", toks["supervisor"])
a = r["data"]
check("档案含消杀与药剂统计", a["treatments_total"] > 0 and len(a["chemical_usage"]) > 0)
check("档案含宠物投诉与告知调整", a["pet_complaints"]["total"] >= 2 and len(a["notify_adjustments"]) >= 1)
check("档案含跟踪结果", a["pet_tracking"]["latest_adjustment"] != "")
# 跟踪进入下次计划（派单预览）
s, r = call("GET", "/api/dispatch/preview", toks["street"])
check("派单预览含宠物投诉跟踪", any(pt["community_name"] == "阳光小区" and pt["total_complaints"] >= 2 for pt in r["data"]["pet_tracking"]))

print("== 15. 物业积水整改（地下室排水沟长期积水） ==")
toks["property2"] = login("property2", "Property@123")

# 15.1 列表 / 超期 / 可见范围
s, r = call("GET", "/api/property-rectifications", toks["street"])
prects = r["data"]
check("积水整改列表（含 3 条种子演示）", s == 200 and len(prects) >= 3, f"({len(prects)} 条)")
pr1 = next(p for p in prects if "B2 集水井" in p["water_location"])
pr2 = next(p for p in prects if "A 区排水沟" in p["water_location"])
pr3 = next(p for p in prects if "B1 排水沟" in p["water_location"])
check("种子①整改超期（待整改且复查日期已过）", pr1["status"] == "pending" and pr1["rect_overdue"])
check("种子②复查超期（已报审待复查且日期已过）", pr2["status"] == "recheck_pending" and pr2["recheck_overdue"])
check("种子③已按图复查通过且投诉 2→0", pr3["status"] == "verified" and pr3["complaints_before"] == 2 and pr3["complaints_after"] == 0 and pr3["complaint_decreased"] is True)
s, r = call("GET", "/api/property-rectifications?overdue=1", toks["street"])
check("仅看超期过滤生效", len(r["data"]) >= 2 and all(p["rect_overdue"] or p["recheck_overdue"] for p in r["data"]))
s, r = call("GET", "/api/property-rectifications", toks["resident"])
check("居民列表为空（无权查看）", s == 200 and r["data"] == [])
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["resident2"])
check("居民查看详情被拒绝(403)", s == 403)
s, r = call("GET", "/api/property-rectifications", toks["property2"])
check("物业2只见滨江任务", all(p["community_name"] == "滨江花园" for p in r["data"]) and any(p["id"] == pr2["id"] for p in r["data"]))
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/start", toks["property2"], {})
check("非设施责任人受理被拒绝(403)", s == 403)

# 15.2 种子①：整改超期 → 受理 → 标注照片完成 → 按图复查通过 → 投诉变化
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/start", toks["property"], {})
check("设施责任人受理", s == 200)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/complete", toks["property"], {
    "repair_desc": "已清掏集水井淤泥", "repair_method": "dredge_drain",
    "rectify_photos": [], "rectify_photo_remark": ""})
check("未传整改照片/标注被拒绝(400)", s == 400)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/complete", toks["property"], {
    "repair_desc": "清掏集水井淤积并更换损坏排水泵，沟通后无积水", "repair_method": "replace_pump",
    "rectify_photos": ["https://example.com/pr1-before.jpg", "https://example.com/pr1-after.jpg"],
    "rectify_photo_remark": "照片①位置：地下车库B2集水井（红圈标注长期积水点），处理方式：抽排清淤；照片②位置：同一集水井，处理方式：更换排水泵后复拍"})
check("物业提交整改完成（照片标明位置+方式）", s == 200)
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["street"])
p1 = r["data"]
check("进入待复查且整改照片/方式已记录", p1["status"] == "recheck_pending" and len(p1["rectify_photos"]) == 2 and p1["repair_method_label"] == "检修/更换排水泵")
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/supervise", toks["street"], {"content": "请尽快安排复查，超期纳入物业考核"})
check("街道督办", s == 200)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/recheck", toks["supervisor"], {
    "pass": True, "recheck_photos": [], "recheck_remark": "无照片"})
check("复查未传复查照片被拒绝(400)", s == 400)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/recheck", toks["supervisor"], {
    "pass": True, "recheck_photos": ["https://example.com/pr1-recheck.jpg"],
    "recheck_remark": "对照整改照片同一位置按图核验，集水井已无积水、无孑孓"})
check("卫生监督按图复查通过", s == 200 and r["data"]["complaints_after"] is not None)
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["street"])
p1 = r["data"]
check("任务复查通过且投诉变化已回写", p1["status"] == "verified" and p1["complaints_before"] >= p1["complaints_after"])
check("督办记录保留", any(m["kind"] == "supervise" for m in p1["reminders"]))
s, r = call("GET", "/api/water-points?type=underground_garage", toks["street"])
check("复查通过后积水点清除", any(w["location_desc"] == "地下车库 B2 集水井" and w["status"] == "cleared" for w in r["data"]))

# 15.3 种子②：复查超期提示街道督办与物业负责人 → 复查；并验收 A/B 投诉归因隔离
s, r = call("GET", f"/api/property-rectifications/{pr2['id']}", toks["street"])
check("复查超期自动生成提示", any(m["kind"] == "recheck_overdue" for m in r["data"]["reminders"]))
# 滨江 A 区任务基线只含 A 区投诉（1 起），同小区 B 区投诉不得计入
check("A任务基线仅计A点（=1），不含B区", r["data"]["complaints_before"] == 1, f"(before={r['data']['complaints_before']})")
s, r = call("POST", f"/api/property-rectifications/{pr2['id']}/complete", toks["property2"], {
    "repair_desc": "x", "repair_method": "dredge_drain", "rectify_photos": ["https://x/1.jpg"], "rectify_photo_remark": "x"})
check("待复查状态不可重复提交整改(409)", s == 409)
# 复查窗口内本就存在 B 区投诉（种子 -4 天，晚于 A 建档 -9 天）：不得计入 A
s, r = call("POST", f"/api/property-rectifications/{pr2['id']}/recheck", toks["street"], {
    "pass": True, "recheck_photos": ["https://example.com/pr2-recheck.jpg"], "recheck_remark": "街道按图核验通过"})
check("街道复查通过（复查超期任务）", s == 200)
check("复查时A任务当前投诉仍只计A点（=0，B区不计入）", r["data"]["complaints_after"] == 0, f"(after={r['data']['complaints_after']})")
# 复查后再新增一起 B 区投诉：A 任务投诉变化必须冻结不变
s, _ = call("POST", "/api/reports", toks["property2"], {
    "type": "basement_damp", "location_desc": "地下车库 B 区排水沟", "nearby_population": "车主", "has_pets": False,
    "description": "复查通过后新增的B区投诉"})
check("复查后新增B区投诉成功", s == 200)
s, r = call("GET", f"/api/property-rectifications/{pr2['id']}", toks["street"])
check("复查后新增B投诉不改变A任务投诉变化（冻结 1→0）",
      r["data"]["complaints_before"] == 1 and r["data"]["complaints_after"] == 0 and r["data"]["complaint_decreased"] is True)
# 同小区阳光的 C 区干扰投诉不得计入 B2/B1（pr1: 2→1，pr3: 2→0）
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["street"])
check("B2任务投诉 2→1（不含B1/C区）", r["data"]["complaints_before"] == 2 and r["data"]["complaints_after"] == 1,
      f"({r['data']['complaints_before']}→{r['data']['complaints_after']})")
s, r = call("GET", f"/api/property-rectifications/{pr3['id']}", toks["street"])
check("B1任务投诉 2→0（复查后新增B1投诉被冻结，不含C区）",
      r["data"]["complaints_before"] == 2 and r["data"]["complaints_after"] == 0)

# 15.4 看板：超期清零、设施责任人维度入考核
s, r = call("GET", "/api/dashboard/closed-loop", toks["street"])
rowmap = {x["community_name"]: x for x in r["data"]["rows"]}
check("闭环看板含积水整改列", "prop_rect_total" in rowmap["阳光小区"] and rowmap["阳光小区"]["prop_rect_verified"] >= 2)
check("超期已随复查清零", rowmap["阳光小区"]["prop_rect_overdue"] == 0 and rowmap["滨江花园"]["prop_rect_overdue"] == 0)
s, r = call("GET", "/api/dashboard/assessment?month=" + datetime.now().strftime("%Y-%m"), toks["street"])
fac = [f for f in r["data"]["facilities"] if f["user_name"] in ("王强", "赵敏")]
check("考核含物业设施责任人维度", len(fac) == 2)
wq = next(f for f in fac if f["user_name"] == "王强")
check("设施责任人复查合格率与投诉变化入考核", wq["rect_verified"] >= 2 and wq["recheck_pass_rate"] == 100.0 and wq["rect_overdue"] == 0)
zm = next(f for f in fac if f["user_name"] == "赵敏")
check("A任务投诉变化冻结入考核（1→0，复查后新增B不影响）", zm["complaints_before"] == 1 and zm["complaints_after"] == 0 and zm["complaint_delta"] == -1,
      f"({zm['complaints_before']}→{zm['complaints_after']})")
# 闭环看板同点归因汇总：阳光 B2(2→1)+B1(2→0)=4→1；滨江 A(1→0)，B 不计
check("闭环看板同点投诉前→后（阳光4→1）", rowmap["阳光小区"]["prop_rect_complaints_before"] == 4 and rowmap["阳光小区"]["prop_rect_complaints_after"] == 1,
      f"({rowmap['阳光小区']['prop_rect_complaints_before']}→{rowmap['阳光小区']['prop_rect_complaints_after']})")
check("闭环看板滨江仅计A点（1→0，B点不计）", rowmap["滨江花园"]["prop_rect_complaints_before"] == 1 and rowmap["滨江花园"]["prop_rect_complaints_after"] == 0)

# 15.5 消杀队重复临时处理 → 自动生成物业整改
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/chemicals/{chem['id']}/restock", toks["street"], {"amount": 100})
future = (datetime.now() + timedelta(days=5)).strftime("%Y-%m-%d")
call("POST", f"/api/teams/1/schedules", toks["street"], {"work_date": future, "shift": "allday", "max_orders": 5})
s, r = call("POST", "/api/reports", toks["property"], {
    "type": "basement_damp", "location_desc": "地下车库 B3 排水沟", "nearby_population": "车主", "has_pets": False,
    "description": "排水沟长期积水返味，消杀后反复"})
check("地下室潮湿上报", s == 200)
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
cand = call("GET", "/api/work-orders?community_id=1&status=assigned", toks["street"])[1]["data"]
cand += call("GET", "/api/work-orders?community_id=1&status=pending", toks["street"])[1]["data"]
b3_oid = None
for o in cand:
    dd = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    if dd.get("water_point") and dd["water_point"]["location_desc"] == "地下车库 B3 排水沟":
        b3_oid = o["id"]
        if o["status"] in ("pending", "assigned") and (not o["team_name"]):
            call("POST", f"/api/work-orders/{b3_oid}/assign", toks["street"], {"team_id": 1, "scheduled_date": future})
        break
check("B3 排水沟已派单", b3_oid is not None, f"(工单 {b3_oid})")
call("POST", f"/api/work-orders/{b3_oid}/start", toks["operator"], {})
s, r = call("POST", f"/api/work-orders/{b3_oid}/property-rectifications", toks["operator"], {})
check("未做临时处理前转整改被拒绝(409)", s == 409)
call("POST", f"/api/work-orders/{b3_oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "B3排水沟", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
dd = call("GET", "/api/work-orders/" + str(b3_oid), toks["street"])[1]["data"]
check("首次临时处理不自动建档", len(dd["property_rectifications"]) == 0)
call("POST", f"/api/work-orders/{b3_oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "B3排水沟再次投药", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
dd = call("GET", "/api/work-orders/" + str(b3_oid), toks["street"])[1]["data"]
autos = dd["property_rectifications"]
check("第2次临时处理自动生成物业整改任务（含责任人/复查日期/临时处理2次）",
      len(autos) == 1 and autos[0]["facility_name"] == "王强" and autos[0]["recheck_date_str"] and "王强" in autos[0]["facility_name"])
auto = autos[0]
check("自动建档进入工单时间线", any("生成物业整改任务" in l["action"] for l in dd["logs"]))
s, r = call("POST", f"/api/work-orders/{b3_oid}/property-rectifications", toks["operator"], {})
check("同一积水点不重复建档(409)", s == 409)
# 非地下室积水点 → 拒绝（动态选取一个未闭环且非地下室积水点的工单）
non_base_oid = None
for o in call("GET", "/api/work-orders", toks["street"])[1]["data"]:
    if o["status"] == "closed" or not o.get("water_point_id"):
        continue
    od = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    wp = od.get("water_point") or {}
    if wp.get("type") not in ("basement_damp", "underground_garage"):
        non_base_oid = o["id"]
        break
check("存在非地下室积水点工单用于负例", non_base_oid is not None)
if non_base_oid:
    s, r = call("POST", f"/api/work-orders/{non_base_oid}/property-rectifications", toks["street"], {})
    check("非地下室积水点转整改被拒绝(400)", s == 400)
# 自动任务流转闭环
call("POST", f"/api/property-rectifications/{auto['id']}/start", toks["property"], {})
s, r = call("POST", f"/api/property-rectifications/{auto['id']}/complete", toks["property"], {
    "repair_desc": "重做B3排水沟找坡", "repair_method": "rebuild_drain",
    "rectify_photos": ["https://example.com/auto-1.jpg"], "rectify_photo_remark": "位置：B3排水沟起点；处理方式：重做找坡"})
check("自动任务物业完成整改", s == 200)
s, r = call("POST", f"/api/property-rectifications/{auto['id']}/recheck", toks["grid"], {
    "pass": True, "recheck_photos": ["https://example.com/auto-rc.jpg"], "recheck_remark": "按图核验无积水"})
check("自动任务复查通过、投诉下降", s == 200 and r["data"]["complaints_after"] == 0)

print("== 16. 重点风险期应急响应与跨小区联防 ==")
# 16.1 列表 / 风险小区 / 种子应急
s, r = call("GET", "/api/emergencies", toks["street"])
emlist = r["data"]
check("应急列表含进行中与已解除历史", len(emlist) >= 2 and any(e["status"] == "active" for e in emlist) and any(e["status"] == "resolved" for e in emlist))
active_em = next(e for e in emlist if e["status"] == "active")
s, r = call("GET", "/api/emergencies/risk/communities", toks["street"])
risks = {x["community_name"]: x for x in r["data"]}
check("风险小区含主疫区(应急)与周边联防(预警)", risks.get("阳光小区", {}).get("risk_level") == "emergency" and risks.get("滨江花园", {}).get("risk_level") == "warning")
# 已解除历史应急的老城厢为常态
check("已解除历史应急小区已降级", "老城厢社区" not in risks or risks["老城厢社区"]["risk_level"] == "normal")
# 居民无权启动应急
s, _ = call("POST", "/api/emergencies", toks["resident"], {"title": "x", "communities": [{"community_id": 1}]})
check("居民无权启动应急(403)", s == 403)

# 16.2 详情：病例轨迹/联防小区/重点积水点/跨小区调度/每日汇总
s, r = call("GET", f"/api/emergencies/{active_em['id']}", toks["street"])
d = r["data"]
check("病例活动轨迹已记录", len(d["cases"]) >= 1 and any("楼顶水箱" in t["place"] for c in d["cases"] for t in c["trajectory"]))
cmap = {c["community_name"]: c for c in d["communities"]}
check("联防小区含主疫区+周边", cmap["阳光小区"]["role"] == "affected" and cmap["滨江花园"]["role"] == "surrounding")
check("重点积水点快照关联病例轨迹", any("邻近病例活动轨迹" in w["link_reason"] for w in d["water_points"]))
check("重点清单覆盖楼顶/轮胎等多类型", {"楼顶水箱", "废旧轮胎"} <= {w["type_label"] for w in d["water_points"]})
check("含跨小区消杀队支援", any(x["resource_type"] == "team" and x["team_name"] for x in d["dispatches"]))
check("含昨日每日汇总", any(x["community_name"] == "全响应合计" and x["summary_date"] for x in d["daily"]))
# 主疫区应急工单五方同单
yang_order = cmap["阳光小区"]["work_order_id"]
od = call("GET", "/api/work-orders/" + str(yang_order), toks["street"])[1]["data"]
roles5 = {p["role"] for p in od["parties"]}
check("应急工单五方（居民/物业/消杀/街道/卫监）同单", {"resident", "property", "operator", "street", "supervisor"} <= roles5, f"({roles5})")
check("应急时间线含启动记录", any("启动应急响应" in l["action"] for l in od["logs"]))
# 被跨小区调度的消杀二队(operator2)可见，一队(operator)暂不可见
s, r = call("GET", "/api/emergencies", toks["operator2"])
check("被支援调度的消杀二队可见应急", any(e["id"] == active_em["id"] for e in r["data"]))
s, r = call("GET", f"/api/emergencies/{active_em['id']}", toks["property2"])
check("周边联防小区物业可见应急", s == 200)

# 16.3 跨小区调度：调一队支援滨江 + 药剂不足拒绝
s, r = call("POST", f"/api/emergencies/{active_em['id']}/dispatch", toks["street"], {
    "resource_type": "team", "team_id": 1, "to_community_id": cmap["滨江花园"]["community_id"],
    "from_community_id": cmap["阳光小区"]["community_id"], "action": "一队抽组支援滨江雨水井"})
check("跨小区调度消杀一队", s == 200)
s, r = call("GET", "/api/emergencies", toks["operator"])
check("被调度后消杀一队可见应急", any(e["id"] == active_em["id"] for e in r["data"]))
chem = call("GET", "/api/chemicals", toks["street"])[1]["data"][0]
s, r = call("POST", f"/api/emergencies/{active_em['id']}/dispatch", toks["street"], {
    "resource_type": "chemical", "chemical_id": chem["id"], "amount": 999999, "to_community_id": cmap["阳光小区"]["community_id"]})
check("药剂调配库存不足被拒绝(409)", s == 409)
s, r = call("POST", f"/api/emergencies/{active_em['id']}/dispatch", toks["street"], {
    "resource_type": "chemical", "chemical_id": chem["id"], "amount": 1.0, "to_community_id": cmap["阳光小区"]["community_id"], "action": "应急药剂调配"})
check("药剂跨小区调配成功并扣库存", s == 200)
# 受援小区不在联防范围 → 拒绝
s, r = call("POST", f"/api/emergencies/{active_em['id']}/dispatch", toks["street"], {
    "resource_type": "grid", "resource_ref": "网格员", "to_community_id": 999})
check("向非联防小区调度被拒绝(4xx)", 400 <= s < 500)

# 16.4 投诉密度突增 → 自动提升风险等级（老城厢，网格员 4 起 72h 内投诉）
for i in range(4):
    call("POST", "/api/reports", toks["grid"], {"community_id": 3, "type": "mosquito_dense",
        "location_desc": f"老城厢夜间蚊虫密集点{i}", "nearby_population": "老旧居民楼", "has_pets": False})
s, r = call("GET", "/api/emergencies/risk/communities", toks["street"])
lao_risk = next((x for x in r["data"] if x["community_name"] == "老城厢社区"), None)
check("72h投诉突增自动提升风险等级", lao_risk and lao_risk["risk_level"] == "elevated" and lao_risk["complaints_72h"] >= 4, f"({lao_risk})")

# 16.5 街道启动新城厢应急（疾控预警）→ 重点清单/复查每日/五方应急工单
s, r = call("POST", "/api/emergencies", toks["street"], {
    "title": "老城厢疾控预警应急", "trigger_type": "cdc_warning", "disease": "登革热",
    "recheck_interval_days": 1, "description": "疾控蚊媒密度预警，旧改工地基坑积水",
    "communities": [{"community_id": 3, "role": "affected", "reason": "疾控预警：旧改工地基坑积水"}],
    "cases": [{"community_id": 3, "case_status": "suspect", "patient_alias": "李某（脱敏）",
               "onset_date": datetime.now().strftime("%Y-%m-%d"),
               "trajectory": [{"time": "08:00", "place": "旧改建筑工地基坑", "note": "工地作业"}]}]})
check("启动疾控预警应急", s == 200 and r["data"]["emerg_no"].startswith("EM"))
new_em = r["data"]["id"]
s, d2 = call("GET", f"/api/emergencies/{new_em}", toks["street"])
nd = d2["data"]
check("新应急复查频次=每日", nd["emergency"]["recheck_interval_days"] == 1)
check("新应急重点清单含建筑工地", any("建筑工地" in w["type_label"] for w in nd["water_points"]))
lao_comm = nd["communities"][0]
lo = call("GET", "/api/work-orders/" + str(lao_comm["work_order_id"]), toks["street"])[1]["data"]
check("新应急工单为高优先级协同状态", lo["order"]["status"] == "escalated" and lo["order"]["priority"] >= 100)
# 应急期间消杀复查期限按小区缩短为 1 天
oid_lao = lo["order"]["id"]
call("POST", f"/api/work-orders/{oid_lao}/assign", toks["street"], {"team_id": 1, "scheduled_date": datetime.now().strftime("%Y-%m-%d")})
call("POST", f"/api/work-orders/{oid_lao}/start", toks["operator"], {})
call("POST", f"/api/work-orders/{oid_lao}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "旧改工地基坑", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
lo = call("GET", "/api/work-orders/" + str(oid_lao), toks["street"])[1]["data"]
due = lo["order"]["recheck_due_at"]
check("应急小区消杀后复查期限=次日（频次提高）", due and due[:10] == (datetime.now() + timedelta(days=1)).strftime("%Y-%m-%d"), f"({due})")

# 16.6 每日汇总
s, r = call("POST", f"/api/emergencies/{new_em}/daily-summary", toks["street"], {})
check("生成每日汇总", s == 200 and "chemical_used" in r["data"])
s, d2 = call("GET", f"/api/emergencies/{new_em}", toks["street"])
check("每日汇总含全响应合计行", any(x["community_name"] == "全响应合计" for x in d2["data"]["daily"]))

# 16.7 解除应急 → 自动降级 + 关闭工单 + 归入小区档案
s, r = call("POST", f"/api/emergencies/{new_em}/resolve", toks["street"], {"note": "积水点清除，降级归档"})
check("解除应急", s == 200)
s, r = call("GET", "/api/emergencies/risk/communities", toks["street"])
lao_risk2 = next((x for x in r["data"] if x["community_name"] == "老城厢社区"), None)
check("解除后无其他进行中应急则自动降级常态", lao_risk2 is None or lao_risk2["risk_level"] == "normal")
lo = call("GET", "/api/work-orders/" + str(oid_lao), toks["street"])[1]["data"]
check("应急工单已关闭", lo["order"]["status"] == "closed")
arc = call("GET", "/api/communities/3/archive", toks["supervisor"])[1]["data"]
check("应急处置归入小区消杀档案（含已解除记录）", any(m["emerg_no"] == nd["emergency"]["emerg_no"] and m["status"] == "resolved" for m in arc["emergencies"]))
check("档案含当前风险等级", arc.get("risk_level_label"))

print("== 17. 居民拒绝入户与入户授权 ==")
# 17.1 列表/种子案例/权限
s, r = call("GET", "/api/access-cases", toks["resident"])
seed_ac = next((x for x in r["data"] if x["case_no"] == "AC00000001"), None)
check("居民可见自己的入户案例（种子·多方沟通中）", seed_ac and seed_ac["status"] == "negotiating")
s, r = call("GET", f"/api/access-cases/{seed_ac['id']}", toks["resident"])
check("案例详情含版本记录(>=2)", s == 200 and len(r["data"]["versions"]) >= 2)
s, r = call("GET", f"/api/access-cases/{seed_ac['id']}", toks["resident2"])
check("他人住户无权查看(403)", s == 403)

# 17.2 新建工单：网格员登记居民拒绝入户 → 五方同单、不强行派单
call("POST", "/api/reports", toks["resident2"], {"type": "mosquito_dense",
    "location_desc": "滨江花园 6 栋 302 室", "nearby_population": "有孕妇", "has_pets": True,
    "description": "户内蚊虫多，担心药剂影响孕妇"})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
# 找到该上报对应工单（含 report）
cand = call("GET", "/api/work-orders?community_id=2", toks["street"])[1]["data"]
ac_oid = None
for o in cand:
    dd = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    if dd.get("report") and "6 栋 302" in dd["report"]["location_desc"]:
        ac_oid = o["id"]; break
check("定位滨江 302 工单", ac_oid is not None)
s, r = call("POST", f"/api/work-orders/{ac_oid}/access-cases", toks["grid"], {
    "reject_reasons": ["pregnant", "pets", "distrust_chemical"], "sensitive_groups": ["pregnant"],
    "pets_desc": "猫1只", "acceptable_times": "周末上午", "acceptable_chemicals": "BTI 低毒",
    "outdoor_allowed": True, "outdoor_areas": ["doorway", "staircase", "sewer"],
    "reject_note": "希望先看告知书"})
check("网格员登记拒绝入户", s == 200 and r["data"]["case_no"].startswith("AC"))
acid = r["data"]["id"]
od = call("GET", "/api/work-orders/" + str(ac_oid), toks["street"])[1]["data"]
roles5 = {p["role"] for p in od["parties"]}
check("拒绝入户五方同单", {"resident","property","operator","street","supervisor"} <= roles5, f"({roles5})")
check("工单生成「居民拒绝入户」异常", any(i["type"] == "resident_refused" and i["status"] == "open" for i in od["issues"]))
check("工单详情含入户案例", len(od["access_cases"]) >= 1)
# 未授权消杀不得入户
s, r = call("POST", f"/api/access-cases/{acid}/start", toks["operator2"], {})
check("未授权不得入户(409)", s == 409)

# 17.3 卫监在重点风险期评估必须入户 + 上门沟通
s, r = call("POST", f"/api/access-cases/{acid}/mandatory-assess", toks["street"], {"required": True, "note": "x"})
check("街道无权做卫监评估(403)", s == 403)
s, r = call("POST", f"/api/access-cases/{acid}/mandatory-assess", toks["supervisor"], {"required": True, "note": "登革热风险期，户内积水风险高，须入户"})
check("卫监评估必须入户（结合风险期）", s == 200 and "风险" in r["data"]["risk_context"])
s, r = call("POST", f"/api/access-cases/{acid}/negotiate", toks["property2"], {
    "note": "物业与网格员上门解释药剂安全性", "witnesses": "楼栋长", "final_opinion": "居民同意周末上午入户"})
check("上门沟通记录见证人/最终意见", s == 200)

# 17.4 住户授权（要素校验 + 仅本人）
s, r = call("POST", f"/api/access-cases/{acid}/authorize", toks["resident2"], {
    "auth_scope": "客厅厨房卫生间", "auth_chemical_name": "苏云金杆菌(BTI)", "auth_concentration": "1:100",
    "auth_safety_interval_hours": 4, "auth_item_cover": True, "auth_pet_avoid": True, "auth_vulnerable_avoid": True,
    "auth_companion": "家属", "auth_photo_consent": False, "auth_notice_delivered": True})
check("未同意拍照留证不可授权(400)", s == 400)
s, r = call("POST", f"/api/access-cases/{acid}/authorize", toks["resident"], {
    "auth_scope": "x", "auth_chemical_name": "BTI", "auth_safety_interval_hours": 4, "auth_photo_consent": True, "auth_notice_delivered": True})
check("非住户本人不可授权(403)", s == 403)
s, r = call("POST", f"/api/access-cases/{acid}/authorize", toks["resident2"], {
    "auth_scope": "客厅、厨房、卫生间及阳台地漏", "auth_chemical_name": "苏云金杆菌(BTI)", "auth_concentration": "1:100",
    "auth_safety_interval_hours": 4, "auth_item_cover": True, "auth_pet_avoid": True, "auth_vulnerable_avoid": True,
    "auth_companion": "家属陪同", "auth_photo_consent": True, "auth_notice_delivered": True})
check("住户授权成功", s == 200)

# 17.5 非本队消杀不得开工；本队（工单需先指派二队）开工
call("POST", f"/api/work-orders/{ac_oid}/assign", toks["street"], {"team_id": 2, "scheduled_date": datetime.now().strftime("%Y-%m-%d")})
s, r = call("POST", f"/api/access-cases/{acid}/start", toks["operator"], {})
check("非本队消杀入户被拒绝(403)", s == 403)
s, r = call("POST", f"/api/access-cases/{acid}/start", toks["operator2"], {})
check("本队消杀入户开始", s == 200)

# 17.6 作业中居民临时反悔 → 暂停、排班保留；须重新授权
s, r = call("POST", f"/api/access-cases/{acid}/withdraw", toks["resident2"], {
    "reason": "孕妇身体不适", "reschedule_date": (datetime.now()+timedelta(days=2)).strftime("%Y-%m-%d"), "resources_kept": True})
check("居民临时反悔暂停并改约", s == 200)
s, r = call("POST", f"/api/access-cases/{acid}/start", toks["operator2"], {})
check("反悔后未重新授权不得入户(409)", s == 409)
# 重新授权 → 开工 → 完成
call("POST", f"/api/access-cases/{acid}/authorize", toks["resident2"], {
    "auth_scope": "客厅、厨房、卫生间", "auth_chemical_name": "苏云金杆菌(BTI)", "auth_concentration": "1:100",
    "auth_safety_interval_hours": 4, "auth_item_cover": True, "auth_pet_avoid": True, "auth_vulnerable_avoid": True,
    "auth_companion": "家属", "auth_photo_consent": True, "auth_notice_delivered": True})
call("POST", f"/api/access-cases/{acid}/start", toks["operator2"], {})
s, r = call("POST", f"/api/access-cases/{acid}/complete", toks["operator2"], {
    "spray_area": "厨房、卫生间、阳台地漏", "warning_sign": True, "warning_removed_at": (datetime.now()+timedelta(hours=4)).isoformat(),
    "completion_photos": ["https://example.com/ac-done.jpg"], "resident_confirmed": True, "pet_avoid_done": True,
    "child_safety_interval_hours": 4})
check("入户作业完成", s == 200)
a = call("GET", f"/api/access-cases/{acid}", toks["resident2"])[1]["data"]
check("居民端可查看作业与撤除/安全间隔", a["status"] == "completed" and a["warning_removed_at"] and a["resident_confirmed"])
check("全流程版本化（拒绝/评估/沟通/授权/反悔/再授权/开工/完成 >=8）", len(a["versions"]) >= 8, f"({len(a['versions'])})")

# 17.7 另一户：仅拒绝入户 → 转外围（物业责任）→ 无法根治风险延续提频
call("POST", "/api/reports", toks["resident2"], {"type": "mosquito_dense",
    "location_desc": "滨江花园 8 栋 101 室", "nearby_population": "老人", "has_pets": False, "description": "拒入户"})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
cand = call("GET", "/api/work-orders?community_id=2", toks["street"])[1]["data"]
ext_oid = None
for o in cand:
    dd = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    if dd.get("report") and "8 栋 101" in dd["report"]["location_desc"]:
        ext_oid = o["id"]; break
s, r = call("POST", f"/api/work-orders/{ext_oid}/access-cases", toks["operator2"], {
    "reject_reasons": ["elderly", "privacy"], "sensitive_groups": ["elderly"], "outdoor_allowed": True,
    "outdoor_areas": ["doorway", "sewer"], "reject_note": "老人拒绝入户，仅同意外围"})
extid = r["data"]["id"]
check("登记第二户拒绝入户", s == 200)
s, r = call("POST", f"/api/access-cases/{extid}/external", toks["street"], {"note": "仅处理门口与下水道外围"})
check("转外围并生成物业责任", s == 200 and r["data"]["rectification_id"])
exta = call("GET", f"/api/access-cases/{extid}", toks["street"])[1]["data"]
check("外围状态与整改关联", exta["status"] == "external_only" and exta["external_rectification_id"])
s, r = call("POST", f"/api/access-cases/{extid}/continue-risk", toks["supervisor"], {
    "note": "户内积水无法根治，公共区域加密复查", "recheck_interval_days": 2})
check("标记风险延续并提频为2天", s == 200 and r["data"]["recheck_interval_days"] == 2)

# 17.8 风险延续纳入考核与小区档案
ass = call("GET", "/api/dashboard/assessment?month=" + datetime.now().strftime("%Y-%m"), toks["street"])[1]["data"]
brow = next(x for x in ass["communities"] if x["community_name"] == "滨江花园")
check("考核含入户拒绝/风险延续指标", brow["access_refused"] >= 2 and brow["access_risk_continued"] >= 1 and brow["access_completed"] >= 1,
      f"(拒绝{brow['access_refused']}, 延续{brow['access_risk_continued']}, 完成{brow['access_completed']})")
arc = call("GET", "/api/communities/2/archive", toks["supervisor"])[1]["data"]
check("小区档案含入户授权汇总与案例", arc["access_cases_summary"]["total"] >= 2 and any(x["case_no"] == "AC%08d" % acid for x in arc["access_cases"]))
check("档案案例含版本数", all(x["version_count"] >= 1 for x in arc["access_cases"]))

print(f"\n结果：{PASS} 通过，{FAIL} 失败")
sys.exit(1 if FAIL else 0)
