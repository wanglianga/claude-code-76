package main

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func registerAuthRoutes() {
	handlePublic("POST /api/auth/login", hLogin)
	handle("POST /api/auth/logout", hLogout)
	handle("GET /api/auth/me", hMe)
}

func hLogin(c *Ctx) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Username == "" || req.Password == "" {
		jsonErr(c.W, 400, "请提供用户名和密码")
		return
	}
	var su SessionUser
	var hash string
	var communityName, teamName *string
	err := db.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.name, u.role, u.community_id, u.team_id,
		       c.name, t.name
		FROM users u
		LEFT JOIN communities c ON c.id = u.community_id
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE u.username = $1`, req.Username).
		Scan(&su.ID, &su.Username, &hash, &su.Name, &su.Role, &su.CommunityID, &su.TeamID, &communityName, &teamName)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		jsonErr(c.W, 401, "用户名或密码错误")
		return
	}
	su.RoleLabel = labelOf(RoleLabels, su.Role)
	if communityName != nil {
		su.CommunityName = *communityName
	}
	if teamName != nil {
		su.TeamName = *teamName
	}
	token := randToken()
	sessSet(token, &su)
	jsonOK(c.W, map[string]any{"token": token, "user": su})
}

func hLogout(c *Ctx) {
	token := strings.TrimPrefix(c.R.Header.Get("Authorization"), "Bearer ")
	sessDel(token)
	jsonOK(c.W, map[string]string{"message": "已退出登录"})
}

func hMe(c *Ctx) {
	jsonOK(c.W, c.User)
}
